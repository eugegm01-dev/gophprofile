package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type Alert struct {
	Status string `json:"status"`
	Labels struct {
		Alertname string `json:"alertname"`
		Severity  string `json:"severity"`
	} `json:"labels"`
	Annotations struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
	} `json:"annotations"`
	StartsAt time.Time `json:"startsAt"`
}

type AlertPayload struct {
	Alerts []Alert `json:"alerts"`
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var payload AlertPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	for _, alert := range payload.Alerts {
		emoji := "⚠️"
		if alert.Labels.Severity == "critical" {
			emoji = "🚨"
		}
		fmt.Printf("\n%s [%s] %s\n", emoji, alert.Status, alert.Labels.Alertname)
		fmt.Printf("   Summary: %s\n", alert.Annotations.Summary)
		fmt.Printf("   Description: %s\n", alert.Annotations.Description)
		fmt.Printf("   Time: %s\n\n", alert.StartsAt.Format(time.RFC3339))
	}

	w.WriteHeader(200)
	w.Write([]byte("OK"))
}

func main() {
	http.HandleFunc("/", webhookHandler)
	log.Println("🔔 Alert webhook listener started on :5001")
	log.Fatal(http.ListenAndServe(":5001", nil))
}
