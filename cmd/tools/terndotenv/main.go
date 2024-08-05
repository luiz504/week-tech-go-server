package main

import (
	"fmt"
	"log"
	"os/exec"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file 💥: %v", err)
	}

	cmd := exec.Command(
		"tern",
		"migrate",
		"--migrations",
		"./internal/store/pg/migrations",
		"--config",
		"./internal/store/pg/migrations/tern.conf",
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		log.Fatalf("Migration command failed 💥: %v\nOutput: %s", err, output)
	} else {
		fmt.Println("Migration successful! 🫐🫐")
	}
}
