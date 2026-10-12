package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port, ClientOrigin, MQTTURL, MQTTClientID, CardFile  string
	CameraMaxUploadBytes                                 int64
	SMTPHost, SMTPUser, SMTPPass, SMTPFrom, EmailSubject string
	SMTPPort                                             int
	SMTPSecure                                           bool
}

func Load() Config {
	return Config{
		Port:                 env("HTTP_PORT", "3000"),
		ClientOrigin:         env("CLIENT_ORIGIN", "http://localhost:3001"),
		MQTTURL:              env("MQTT_BROKER_URL", "tcp://localhost:1883"),
		MQTTClientID:         env("MQTT_CLIENT_ID", "smartlock-admin-server"),
		CardFile:             env("CARD_FILE", "./data/cards.json"),
		CameraMaxUploadBytes: int64(envInt("CAMERA_MAX_UPLOAD_BYTES", 1024*1024)),
		SMTPHost:             env("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:             envInt("SMTP_PORT", 465),
		SMTPSecure:           env("SMTP_SECURE", "true") == "true",
		SMTPUser:             os.Getenv("SMTP_USER"), SMTPPass: os.Getenv("SMTP_PASS"),
		SMTPFrom: os.Getenv("SMTP_FROM"), EmailSubject: env("EMAIL_SUBJECT", "Your Smart Lock OTP"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}
