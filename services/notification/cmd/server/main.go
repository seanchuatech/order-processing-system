package main

import (
	"log"
	"time"
)

func main() {
	log.Println("Starting notification-service consumer loop...")
	for {
		log.Println("Waiting for events...")
		time.Sleep(10 * time.Second)
	}
}
