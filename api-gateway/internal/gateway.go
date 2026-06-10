package internal

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"e2ee/pkg/jwtutils"
	keyspb "e2ee/pkg/pb/keyspb"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Gateway struct {
	keysClient  keyspb.KeysServiceClient
	redisClient *redis.Client
	kafkaWriter *kafka.Writer
	jwtSecret   string
}

func NewGateway(keysConn *grpc.ClientConn, rdb *redis.Client, kafkaWriter *kafka.Writer, jwtSecret string) *Gateway {
	return &Gateway{
		keysClient:  keyspb.NewKeysServiceClient(keysConn),
		redisClient: rdb,
		kafkaWriter: kafkaWriter,
		jwtSecret:   jwtSecret,
	}
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/keys", otelhttp.NewHandler(http.HandlerFunc(g.handleRegister), "RegisterUser"))
	mux.Handle("/keys/", otelhttp.NewHandler(http.HandlerFunc(g.handleGetPublicKey), "GetPublicKey"))
	mux.Handle("/ws", otelhttp.NewHandler(http.HandlerFunc(g.handleWebSocket), "WebSocket"))
	return mux
}

func (g *Gateway) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID                string `json:"id"`
		IdentityPublicKey string `json:"identityPublicKey"`
		DhPublicKey       string `json:"dhPublicKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	resp, err := g.keysClient.RegisterUser(r.Context(), &keyspb.RegisterUserRequest{
		Id:                req.ID,
		IdentityPublicKey: req.IdentityPublicKey,
		DhPublicKey:       req.DhPublicKey,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"token": resp.Token})
}

func (g *Gateway) handleGetPublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/keys/")
	resp, err := g.keysClient.GetPublicKey(r.Context(), &keyspb.GetPublicKeyRequest{Id: id})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":                resp.Id,
		"identityPublicKey": resp.IdentityPublicKey,
		"dhPublicKey":       resp.DhPublicKey,
	})
}

type WSMessage struct {
	Type    string `json:"type"`
	Token   string `json:"token,omitempty"`
	Payload struct {
		To                 string `json:"to,omitempty"`
		EphemeralPublicKey string `json:"ephemeralPublicKey,omitempty"`
		EncryptedPayload   string `json:"encryptedPayload,omitempty"`
		Signature          string `json:"signature,omitempty"`
		From               string `json:"from,omitempty"`
	} `json:"payload,omitempty"`
	Message string `json:"message,omitempty"`
}

type KafkaMessage struct {
	From               string `json:"from"`
	To                 string `json:"to"`
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	EncryptedPayload   string `json:"encrypted_payload"`
	Signature          string `json:"signature"`
}

func (g *Gateway) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	var authMsg WSMessage
	if err := conn.ReadJSON(&authMsg); err != nil {
		return
	}

	if authMsg.Token == "" {
		conn.WriteJSON(WSMessage{Type: "error", Message: "token required"})
		return
	}

	userID, err := jwtutils.ValidateToken(authMsg.Token, g.jwtSecret)
	if err != nil {
		conn.WriteJSON(WSMessage{Type: "error", Message: "invalid token"})
		return
	}

	slog.Info("user connected", "user_id", userID)
	conn.WriteJSON(WSMessage{Type: "auth_success"})

	g.redisClient.Set(context.Background(), "user:"+userID+":status", "online", 0)
	defer g.redisClient.Del(context.Background(), "user:"+userID+":status")

	pubsub := g.redisClient.Subscribe(context.Background(), "channel:"+userID)
	defer pubsub.Close()

	stopCh := make(chan struct{})
	defer close(stopCh)

	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-stopCh:
				return
			case msg := <-ch:
				var incoming WSMessage
				if err := json.Unmarshal([]byte(msg.Payload), &incoming); err == nil {
					conn.WriteJSON(incoming)
				}
			}
		}
	}()

	for {
		var wsMsg WSMessage
		err := conn.ReadJSON(&wsMsg)
		if err != nil {
			break
		}

		if wsMsg.Type == "send_message" {
			kafkaPayload := KafkaMessage{
				From:               userID,
				To:                 wsMsg.Payload.To,
				EphemeralPublicKey: wsMsg.Payload.EphemeralPublicKey,
				EncryptedPayload:   wsMsg.Payload.EncryptedPayload,
				Signature:          wsMsg.Payload.Signature,
			}
			kb, _ := json.Marshal(kafkaPayload)

			err := g.kafkaWriter.WriteMessages(context.Background(), kafka.Message{
				Key:   []byte(wsMsg.Payload.To), // Partition by recipient
				Value: kb,
			})
			if err != nil {
				conn.WriteJSON(WSMessage{Type: "error", Message: "failed to send message"})
				slog.Error("failed to write to kafka", "error", err)
				continue
			}

			outMsg := WSMessage{
				Type: "new_message",
				Payload: struct {
					To                 string `json:"to,omitempty"`
					EphemeralPublicKey string `json:"ephemeralPublicKey,omitempty"`
					EncryptedPayload   string `json:"encryptedPayload,omitempty"`
					Signature          string `json:"signature,omitempty"`
					From               string `json:"from,omitempty"`
				}{
					From:               userID,
					To:                 wsMsg.Payload.To,
					EphemeralPublicKey: wsMsg.Payload.EphemeralPublicKey,
					EncryptedPayload:   wsMsg.Payload.EncryptedPayload,
					Signature:          wsMsg.Payload.Signature,
				},
			}

			b, _ := json.Marshal(outMsg)
			g.redisClient.Publish(context.Background(), "channel:"+wsMsg.Payload.To, string(b))
		}
	}
}
