// Package broker is the ingestion layer between store controllers and the
// diagnostics engine. In production this is EMQX (MQTT); the demo ships an
// in-memory implementation behind the same interface.
package broker

import (
	"fmt"
	"strings"
)

type Message struct {
	Topic   string
	Payload []byte
}

type Handler func(Message)

// Broker is the minimal pub/sub contract the rest of the system depends on.
// Topic filters use MQTT wildcard syntax: '+' matches one level, '#' the rest.
type Broker interface {
	Publish(topic string, payload []byte) error
	Subscribe(filter string, h Handler) (unsubscribe func(), err error)
	Close()
}

// ---- Topic helpers (spec §2.2) --------------------------------------------

const (
	KindTelemetry = "telemetry"
	KindEvents    = "events"
	KindCommands  = "commands"
)

func Topic(storeID, controllerID, kind string) string {
	return fmt.Sprintf("rocateq/store/%s/controller/%s/%s", storeID, controllerID, kind)
}

// Filter subscribes to one message kind across all stores and controllers.
func Filter(kind string) string {
	return "rocateq/store/+/controller/+/" + kind
}

// ParseTopic extracts the ids from a rocateq topic.
func ParseTopic(topic string) (storeID, controllerID, kind string, ok bool) {
	p := strings.Split(topic, "/")
	if len(p) != 6 || p[0] != "rocateq" || p[1] != "store" || p[3] != "controller" {
		return "", "", "", false
	}
	return p[2], p[4], p[5], true
}

// Match reports whether an MQTT topic filter matches a concrete topic.
func Match(filter, topic string) bool {
	f := strings.Split(filter, "/")
	t := strings.Split(topic, "/")
	for i, seg := range f {
		if seg == "#" {
			return true
		}
		if i >= len(t) {
			return false
		}
		if seg != "+" && seg != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}
