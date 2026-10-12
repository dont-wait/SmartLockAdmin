package mqtt

import (
	"log"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type Handler func(topic, payload string)

func Connect(broker, clientID string, handler Handler) (paho.Client, error) {
	opts := paho.NewClientOptions().AddBroker(broker).SetClientID(clientID).SetAutoReconnect(true).SetConnectRetry(true).SetConnectRetryInterval(2 * time.Second)
	opts.OnConnect = func(c paho.Client) {
		for _, topic := range []string{"smartlock/device/+/otp/request", "smartlock/device/+/card/enroll", "smartlock/device/+/card/verify"} {
			t := topic
			if token := c.Subscribe(t, 0, func(_ paho.Client, m paho.Message) { handler(m.Topic(), string(m.Payload())) }); token.Wait() && token.Error() != nil {
				log.Printf("MQTT subscribe %s: %v", t, token.Error())
			}
		}
	}
	client := paho.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(15 * time.Second) {
		return nil, contextDeadlineError{}
	}
	if err := token.Error(); err != nil {
		return nil, err
	}
	return client, nil
}

type contextDeadlineError struct{}

func (contextDeadlineError) Error() string { return "MQTT connection timed out" }

func Publish(client paho.Client, topic, payload string) {
	if token := client.Publish(topic, 1, false, payload); token.Wait() && token.Error() != nil {
		log.Printf("MQTT publish %s: %v", topic, token.Error())
	}
}
