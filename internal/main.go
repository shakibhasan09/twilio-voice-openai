package internal

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

var (
	port          string
	openAIAPIKey  string
	systemMessage string
	xmlResponse   string
	upgrader      = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	logEventTypes = []string{
		"response.content.done",
		"rate_limits.updated",
		"response.done",
		"input_audio_buffer.committed",
		"input_audio_buffer.speech_stopped",
		"input_audio_buffer.speech_started",
		"session.created",
	}
)

func Run() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	openAIAPIKey = os.Getenv("OPENAI_API_KEY")
	if openAIAPIKey == "" {
		log.Fatal("Missing OpenAI API key. Please set it in the .env file.")
	}

	systemMessage = os.Getenv("SYSTEM_MESSAGE")
	if systemMessage == "" {
		log.Fatal("Missing system message. Please set it in the .env file.")
	}

	port = os.Getenv("PORT")
	if port == "" {
		log.Fatal("Missing port. Please set it in the .env file.")
	}

	xmlResponse = os.Getenv("TWILIO_XML_RESPONSE")
	if xmlResponse == "" {
		log.Fatal("Missing Twilio XML response. Please set it in the .env file.")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/incoming-call", handleIncomingCall)
	mux.HandleFunc("/media-stream", handleMediaStream)

	log.Printf("Server is listening on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Twilio Media Stream Server is running!"})
}

func handleIncomingCall(w http.ResponseWriter, r *http.Request) {
	twimlResponse := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
		<Response>
			<Say>%s</Say>
			<Pause length="1"/>
			<Say>O.K. you can start talking!</Say>
			<Connect>
				<Stream url="wss://%s/media-stream" />
			</Connect>
		</Response>`, xmlResponse, r.Host)

	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(twimlResponse))
}

func handleMediaStream(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:", err)
		return
	}
	defer ws.Close()

	log.Println("Client connected")

	openAIWs, _, err := websocket.DefaultDialer.Dial("wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01", http.Header{
		"Authorization": []string{"Bearer " + openAIAPIKey},
		"OpenAI-Beta":   []string{"realtime=v1"},
	})
	if err != nil {
		log.Println("Error connecting to OpenAI WebSocket:", err)
		return
	}
	defer openAIWs.Close()

	log.Println("Connected to the OpenAI Realtime API")

	var streamSid string

	go handleOpenAIMessages(openAIWs, ws, &streamSid)

	// Send session update
	sessionUpdate := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"turn_detection":      map[string]string{"type": "server_vad"},
			"input_audio_format":  "g711_ulaw",
			"output_audio_format": "g711_ulaw",
			"voice":               "alloy",
			"instructions":        systemMessage,
			"modalities":          []string{"text", "audio"},
			"temperature":         0.8,
		},
	}
	if err := openAIWs.WriteJSON(sessionUpdate); err != nil {
		log.Println("Error sending session update:", err)
		return
	}

	handleTwilioMessages(ws, openAIWs, &streamSid)
}

func handleOpenAIMessages(openAIWs, twilioWs *websocket.Conn, streamSid *string) {
	for {
		_, message, err := openAIWs.ReadMessage()
		if err != nil {
			log.Println("Error reading from OpenAI WebSocket:", err)
			return
		}

		var response map[string]interface{}
		if err := json.Unmarshal(message, &response); err != nil {
			log.Println("Error unmarshaling OpenAI message:", err)
			continue
		}

		responseType, _ := response["type"].(string)
		log.Printf("Received OpenAI message: %s\n", responseType)

		for _, eventType := range logEventTypes {
			if responseType == eventType {
				log.Printf("Received event: %s %v\n", responseType, response)
				break
			}
		}

		if responseType == "session.updated" {
			log.Println("Session updated successfully:", response)
		} else if responseType == "response.audio.delta" {
			if delta, ok := response["delta"].(string); ok {
				audioDelta := map[string]interface{}{
					"event":     "media",
					"streamSid": *streamSid,
					"media": map[string]string{
						"payload": delta,
					},
				}
				if err := twilioWs.WriteJSON(audioDelta); err != nil {
					log.Println("Error sending audio delta to Twilio:", err)
				}
			}
		}
	}
}

func handleTwilioMessages(twilioWs, openAIWs *websocket.Conn, streamSid *string) {
	for {
		_, message, err := twilioWs.ReadMessage()
		if err != nil {
			log.Println("Error reading from Twilio WebSocket:", err)
			return
		}

		var data map[string]interface{}
		if err := json.Unmarshal(message, &data); err != nil {
			log.Println("Error unmarshaling Twilio message:", err)
			continue
		}

		event, _ := data["event"].(string)
		switch event {
		case "media":
			media, _ := data["media"].(map[string]interface{})
			payload, _ := media["payload"].(string)
			audioAppend := map[string]interface{}{
				"type":  "input_audio_buffer.append",
				"audio": payload,
			}
			if err := openAIWs.WriteJSON(audioAppend); err != nil {
				log.Println("Error sending audio append to OpenAI:", err)
			}
		case "start":
			start, _ := data["start"].(map[string]interface{})
			*streamSid, _ = start["streamSid"].(string)
			log.Println("Incoming stream has started", *streamSid)
		default:
			log.Println("Received non-media event:", event)
		}
	}
}
