package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

var (
	config struct {
		Port          string
		OpenAIAPIKey  string
		SystemMessage string
		XMLResponse   string
		WebhookURL    string
	}
	upgrader      = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	logEventTypes = map[string]struct{}{
		"response.content.done":             {},
		"rate_limits.updated":               {},
		"response.done":                     {},
		"input_audio_buffer.committed":      {},
		"input_audio_buffer.speech_stopped": {},
		"input_audio_buffer.speech_started": {},
		"session.created":                   {},
	}
)

func Run() {
	loadConfig()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/incoming-call", handleIncomingCall)
	mux.HandleFunc("/media-stream/{number}", handleMediaStream)

	log.Printf("Server is listening on port %s\n", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, mux))
}

func loadConfig() {
	if os.Getenv("GO_ENV") == "development" {
		if err := godotenv.Load(); err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	config.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	config.SystemMessage = os.Getenv("SYSTEM_MESSAGE")
	config.Port = os.Getenv("PORT")
	config.XMLResponse = os.Getenv("GREETINGS_RESPONSE")
	config.WebhookURL = os.Getenv("WEBHOOK_URL")

	if config.OpenAIAPIKey == "" || config.SystemMessage == "" || config.Port == "" || config.XMLResponse == "" || config.WebhookURL == "" {
		log.Fatal("Missing required environment variables. Please check your .env file.")
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"message": "Twilio Media Stream Server is running!"})
}

func handleIncomingCall(w http.ResponseWriter, r *http.Request) {
	twimlResponse := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
		<Response>
			<Connect>
				<Stream url="wss://%s/media-stream/%s" />
			</Connect>
		</Response>`, r.Host, r.FormValue("From"))

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

	openAIWs, _, err := websocket.DefaultDialer.Dial("wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01", http.Header{
		"Authorization": []string{"Bearer " + config.OpenAIAPIKey},
		"OpenAI-Beta":   []string{"realtime=v1"},
	})
	if err != nil {
		log.Println("Error connecting to OpenAI WebSocket:", err)
		return
	}
	defer openAIWs.Close()

	var streamSid string
	var wg sync.WaitGroup
	wg.Add(2)

	go handleOpenAIMessages(openAIWs, ws, &streamSid, r.PathValue("number"), &wg)
	go handleTwilioMessages(ws, openAIWs, &streamSid, &wg)

	if err := sendInitialMessages(openAIWs); err != nil {
		log.Println("Error sending initial messages:", err)
		return
	}

	wg.Wait()
}

func sendInitialMessages(openAIWs *websocket.Conn) error {
	messages := []map[string]interface{}{
		{
			"type": "session.update",
			"session": map[string]interface{}{
				"turn_detection":      map[string]string{"type": "server_vad"},
				"input_audio_format":  "g711_ulaw",
				"output_audio_format": "g711_ulaw",
				"voice":               "alloy",
				"instructions":        config.SystemMessage,
				"modalities":          []string{"text", "audio"},
				"temperature":         0.8,
				"tools": []map[string]interface{}{
					{
						"type":        "function",
						"name":        "setup_schedule",
						"description": "Setup business meeting schedule",
						"parameters": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":        map[string]string{"type": "string", "description": "Please tell me your name"},
								"email":       map[string]string{"format": "email", "type": "string", "description": "please provide your email address"},
								"datetime":    map[string]string{"type": "string", "format": "date-time", "description": "Please provide the date and time of the meeting"},
								"description": map[string]string{"type": "string", "description": "what is the purpose of the meeting?"},
							},
							"required": []string{"name", "email", "description"},
						},
					},
				},
			},
		},
		{
			"type": "conversation.item.create",
			"item": map[string]interface{}{
				"id":   "greeting_01",
				"type": "message",
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "text", "text": config.XMLResponse},
				},
			},
		},
		{"type": "response.create"},
	}

	for _, msg := range messages {
		if err := openAIWs.WriteJSON(&msg); err != nil {
			return fmt.Errorf("error sending message: %v", err)
		}
	}

	return nil
}

func handleOpenAIMessages(openAIWs, twilioWs *websocket.Conn, streamSid *string, phoneNumber string, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		var response map[string]interface{}
		if err := openAIWs.ReadJSON(&response); err != nil {
			log.Println("Error reading from OpenAI WebSocket:", err)
			return
		}

		responseType, _ := response["type"].(string)
		if _, ok := logEventTypes[responseType]; ok {
			log.Printf("Received OpenAI message: %s\n", responseType)
		}

		if responseType == "error" {
			log.Printf("OpenAI error: %v\n", response)
			continue
		}

		if responseType == "response.audio.delta" {
			if delta, ok := response["delta"].(string); ok {
				audioDelta := map[string]interface{}{
					"event":     "media",
					"streamSid": *streamSid,
					"media":     map[string]string{"payload": delta},
				}
				if err := twilioWs.WriteJSON(audioDelta); err != nil {
					log.Println("Error sending audio delta to Twilio:", err)
				}
			}
		}

		if resp, ok := response["response"].(map[string]interface{}); ok {
			handleOpenAIResponse(resp, openAIWs, phoneNumber)
		}
	}
}

func handleOpenAIResponse(response map[string]interface{}, openAIWs *websocket.Conn, phoneNumber string) {
	output, ok := response["output"].([]interface{})
	if !ok || len(output) == 0 {
		return
	}

	firstOutput, ok := output[0].(map[string]interface{})
	if !ok {
		return
	}

	outputType, _ := firstOutput["type"].(string)
	name, _ := firstOutput["name"].(string)
	arguments, _ := firstOutput["arguments"].(string)
	callID, _ := firstOutput["call_id"].(string)

	if outputType == "function_call" && name == "setup_schedule" {
		var data map[string]string
		if err := json.Unmarshal([]byte(arguments), &data); err != nil {
			log.Println("Error parsing JSON:", err)
			return
		}

		if err := setupSchedule(data["name"], data["email"], data["datetime"], data["description"], phoneNumber); err != nil {
			log.Println("Error setting up schedule:", err)
			return
		}

		webhookResponse := map[string]interface{}{
			"type": "conversation.item.create",
			"item": map[string]interface{}{
				"call_id": callID,
				"type":    "function_call_output",
				"output":  "Your schedule has been set successfully!",
			},
		}
		if err := openAIWs.WriteJSON(webhookResponse); err != nil {
			log.Println("Error sending webhook response to OpenAI:", err)
		}

		responseCreate := map[string]interface{}{"type": "response.create"}
		if err := openAIWs.WriteJSON(&responseCreate); err != nil {
			log.Println("Error sending response create:", err)
		}
	}
}

func handleTwilioMessages(twilioWs, openAIWs *websocket.Conn, streamSid *string, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		var data map[string]interface{}
		if err := twilioWs.ReadJSON(&data); err != nil {
			log.Println("Error reading from Twilio WebSocket:", err)
			return
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

func setupSchedule(name, email, datetime, description, phoneNumber string) error {
	data := struct {
		Name        string `json:"name"`
		Email       string `json:"email"`
		DateTime    string `json:"datetime"`
		Description string `json:"description"`
		PhoneNumber string `json:"phone_number"`
	}{
		Name: name, Email: email, DateTime: datetime,
		Description: description, PhoneNumber: phoneNumber,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %v", err)
	}

	resp, err := http.Post(config.WebhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}
