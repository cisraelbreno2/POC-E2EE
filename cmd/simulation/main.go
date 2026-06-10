package main

import (
	"fmt"
	"log"
	"os"

	"e2ee/simulation"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load(".env", "../../.env")

	gatewayURL := os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		gatewayURL = "http://localhost:8080"
	}

	fmt.Println("🚀 Iniciando simulação E2EE em tempo real...")
	runner := simulation.NewRunner(gatewayURL)
	if err := runner.Run(); err != nil {
		log.Fatalf("❌ Falha na simulação: %v", err)
	}
	fmt.Println("🎉 Simulação concluída com sucesso!")
}
