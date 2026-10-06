package main

import (
	"log"
	"os"
	"uptime-app/backend/internal/openapi"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("provide generated contract directory")
	}
	if err := openapi.NormalizeNullable(os.Args[1]); err != nil {
		log.Fatal(err)
	}
}
