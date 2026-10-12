package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/websocket"
	"smartlockadmin/server/internal/config"
	"smartlockadmin/server/internal/mailer"
	"smartlockadmin/server/internal/model"
	mqttclient "smartlockadmin/server/internal/mqtt"
	"smartlockadmin/server/internal/otp"
	"smartlockadmin/server/internal/store"
)

type app struct {
	cfg     config.Config
	cards   *store.Cards
	otp     *otp.Service
	mqtt    paho.Client
	events  *webHub
	frameMu sync.RWMutex
	frame   []byte
}

// webHub wraps the internal event hub while keeping route dependencies local.
type webHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newWebHub() *webHub { return &webHub{clients: map[chan []byte]struct{}{}} }
func (h *webHub) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.mu.Lock(); delete(h.clients, ch); h.mu.Unlock() }
}
func (h *webHub) publish(cards []model.Card) {
	b, err := json.Marshal(map[string]any{"cards": cards})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- b
		}
	}
}

func main() {
	cfg := config.Load()
	cards, err := store.OpenCards(cfg.CardFile)
	if err != nil {
		log.Fatal(err)
	}
	a := &app{cfg: cfg, cards: cards, otp: otp.New(), events: newWebHub()}
	a.mqtt, err = mqttclient.Connect(cfg.MQTTURL, cfg.MQTTClientID, a.handleMQTT)
	if err != nil {
		log.Fatalf("MQTT connection: %v", err)
	}
	defer a.mqtt.Disconnect(250)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("SmartLockAdmin API listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown: %v", err)
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /", func(w http.ResponseWriter, r *http.Request) { a.cors(w); w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/cards", func(w http.ResponseWriter, r *http.Request) { a.json(w, 200, map[string]any{"cards": a.cards.List()}) })
	mux.HandleFunc("PUT /api/cards", a.replaceCards)
	mux.HandleFunc("GET /api/cards/events", a.cardEventsSSE)
	mux.HandleFunc("GET /api/cards/ws", a.cardEventsWS)
	mux.HandleFunc("POST /api/camera/frame", a.uploadFrame)
	mux.HandleFunc("GET /api/camera/frame", a.getFrame)
	mux.HandleFunc("POST /api/otp", a.issueOTP)
	mux.HandleFunc("POST /api/otp/verify", a.verifyOTP)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.cors(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != a.cfg.ClientOrigin {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *app) cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", a.cfg.ClientOrigin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
func (a *app) json(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (a *app) badRequest(w http.ResponseWriter, message string) {
	a.json(w, http.StatusBadRequest, map[string]string{"error": message})
}
func (a *app) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		a.badRequest(w, "invalid JSON body")
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		a.badRequest(w, "invalid JSON body")
		return false
	}
	return true
}

func (a *app) replaceCards(w http.ResponseWriter, r *http.Request) {
	var req model.CardsRequest
	if !a.decode(w, r, &req) {
		return
	}
	for _, card := range req.Cards {
		if strings.TrimSpace(card.Uid) == "" {
			a.badRequest(w, "invalid cards")
			return
		}
	}
	if err := a.cards.Replace(req.Cards); err != nil {
		log.Printf("card save: %v", err)
		a.json(w, 500, map[string]string{"error": "failed to update cards"})
		return
	}
	cards := a.cards.List()
	a.events.publish(cards)
	a.json(w, 200, map[string]any{"cards": cards})
}

func (a *app) cardEventsSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	_, _ = fmt.Fprint(w, ": connected\n\n")
	ch, unsub := a.events.subscribe()
	defer unsub()
	send := func(b []byte) { _, _ = fmt.Fprintf(w, "data: %s\n\n", b); f.Flush() }
	initial, _ := json.Marshal(map[string]any{"cards": a.cards.List()})
	send(initial)
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			send(b)
		}
	}
}

func (a *app) cardEventsWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == a.cfg.ClientOrigin
	}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ch, unsub := a.events.subscribe()
	defer unsub()
	initial, _ := json.Marshal(map[string]any{"cards": a.cards.List()})
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err = conn.WriteMessage(websocket.TextMessage, initial); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case b := <-ch:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		}
	}
}

func (a *app) uploadFrame(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "image/jpeg" {
		a.json(w, 415, map[string]string{"error": "Content-Type must be image/jpeg"})
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, a.cfg.CameraMaxUploadBytes))
	if err != nil {
		a.json(w, 413, map[string]string{"error": "camera frame too large"})
		return
	}
	if len(b) == 0 {
		a.badRequest(w, "empty camera frame")
		return
	}
	a.frameMu.Lock()
	a.frame = b
	a.frameMu.Unlock()
	a.json(w, 200, map[string]any{"success": true, "size": len(b)})
}
func (a *app) getFrame(w http.ResponseWriter, r *http.Request) {
	a.frameMu.RLock()
	b := append([]byte(nil), a.frame...)
	a.frameMu.RUnlock()
	if len(b) == 0 {
		a.json(w, 404, map[string]string{"error": "no camera frame"})
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	_, _ = w.Write(b)
}

func (a *app) issueOTP(w http.ResponseWriter, r *http.Request) {
	var req model.IssueOtpRequest
	if !a.decode(w, r, &req) {
		return
	}
	if req.DeviceId == "" || !strings.Contains(string(req.Email), "@") {
		a.badRequest(w, "deviceId and valid email are required")
		return
	}
	record, err := a.otp.Issue(req.DeviceId, string(req.Email))
	if err != nil {
		a.json(w, 500, map[string]string{"error": "failed to generate OTP"})
		return
	}
	a.notifyOTP(record)
	a.json(w, 200, map[string]any{"deviceId": req.DeviceId, "expiresAt": record.ExpiresAt})
}
func (a *app) verifyOTP(w http.ResponseWriter, r *http.Request) {
	var req model.VerifyOtpRequest
	if !a.decode(w, r, &req) {
		return
	}
	if req.DeviceId == "" || req.Code == "" {
		a.badRequest(w, "deviceId and code are required")
		return
	}
	a.json(w, 200, map[string]bool{"valid": a.otp.Verify(req.DeviceId, req.Code)})
}
func (a *app) notifyOTP(record otp.Record) {
	mqttclient.Publish(a.mqtt, "smartlock/device/"+record.DeviceID+"/otp", record.Code)
	if err := mailer.Send(a.cfg, record); err != nil {
		log.Printf("OTP email delivery failed: %v", err)
	}
}

func (a *app) handleMQTT(topic, payload string) {
	parts := strings.Split(topic, "/")
	if len(parts) < 5 {
		return
	}
	deviceID := parts[2]
	switch {
	case strings.HasSuffix(topic, "/card/enroll"):
		added, err := a.cards.Enroll(payload)
		if err != nil {
			log.Printf("card enroll: %v", err)
			return
		}
		result := "duplicate"
		if added {
			result = "ok"
		}
		mqttclient.Publish(a.mqtt, "smartlock/device/"+deviceID+"/card/enroll/result", result)
		if added {
			a.events.publish(a.cards.List())
		}
	case strings.HasSuffix(topic, "/card/verify"):
		card, ok := a.cards.Find(payload)
		result := "invalid"
		if ok {
			result = "valid"
		}
		mqttclient.Publish(a.mqtt, "smartlock/device/"+deviceID+"/card/verify/result", result)
		if ok && card.Email != "" {
			record, err := a.otp.Issue(deviceID, card.Email)
			if err == nil {
				a.notifyOTP(record)
			}
		}
	case strings.HasSuffix(topic, "/otp/request"):
		record, err := a.otp.Issue(deviceID, a.cfg.SMTPUser)
		if err == nil {
			a.notifyOTP(record)
		}
	}
}
