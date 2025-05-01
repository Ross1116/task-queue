package task

import (
	"log"
	"os"
)

const (
	RabbitMQURLKey = "RABBITMQ_URL"
	DatabaseURLKey = "DATABASE_URL"
	PortKey        = "PORT"
)

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		log.Printf("INFO: Using value from environment variable %s", key)
		return value
	}
	if fallback != "" {
		log.Printf("INFO: Environment variable %s not set. Using default value.", key)
	} else {
		log.Printf("WARN: Environment variable %s not set and no default value provided.", key)
	}
	return fallback
}
