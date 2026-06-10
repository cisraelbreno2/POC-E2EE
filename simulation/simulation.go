package simulation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"e2ee/pkg/clientcrypto"

	"github.com/gorilla/websocket"
)

type Runner struct {
	BaseURL string
	Client  *http.Client
}

func NewRunner(baseURL string) *Runner {
	return &Runner{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (r *Runner) Run() error {
	aliceIdentityPriv, aliceIdentityPub, err := clientcrypto.GenerateEd25519KeyPair()
	if err != nil {
		return err
	}
	aliceDhPriv, aliceDhPub, err := clientcrypto.GenerateX25519KeyPair()
	if err != nil {
		return err
	}

	bobIdentityPriv, bobIdentityPub, err := clientcrypto.GenerateEd25519KeyPair()
	if err != nil {
		return err
	}
	bobDhPriv, bobDhPub, err := clientcrypto.GenerateX25519KeyPair()
	if err != nil {
		return err
	}

	runID := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	aliceID := "Alice-" + runID
	bobID := "Bob-" + runID

	aliceToken, err := r.registerUser(aliceID, aliceIdentityPub, aliceDhPub)
	if err != nil {
		return fmt.Errorf("alice registration failed: %w", err)
	}

	bobToken, err := r.registerUser(bobID, bobIdentityPub, bobDhPub)
	if err != nil {
		return fmt.Errorf("bob registration failed: %w", err)
	}

	_, bobServerDhKey, err := r.fetchUserPublicKey(bobID)
	if err != nil {
		return err
	}

	wsURL := strings.Replace(r.BaseURL, "http://", "ws://", 1) + "/ws"
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("bob ws dial failed: %w", err)
	}
	defer wsConn.Close()

	if err := wsConn.WriteJSON(map[string]interface{}{"token": bobToken}); err != nil {
		return err
	}
	var authResp map[string]interface{}
	wsConn.ReadJSON(&authResp)
	if authResp["type"] != "auth_success" {
		return fmt.Errorf("bob ws auth failed: %v", authResp)
	}

	plaintext := "Olá, Bob, este é um segredo E2EE em TEMPO REAL (PFS via ECDHE)!"

	aesKey, ephemeralPubBase64, err := clientcrypto.EcdheDeriveKeySender(bobServerDhKey)
	if err != nil {
		return fmt.Errorf("alice ecdhe failed: %w", err)
	}

	encryptedPayload, err := clientcrypto.EncryptMessageAES(plaintext, aesKey)
	if err != nil {
		return err
	}

	signature, err := clientcrypto.SignMessageEd25519(encryptedPayload, aliceIdentityPriv)
	if err != nil {
		return err
	}

	aliceWsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer aliceWsConn.Close()
	aliceWsConn.WriteJSON(map[string]interface{}{"token": aliceToken})
	aliceWsConn.ReadJSON(&authResp)

	msgToSend := map[string]interface{}{
		"type": "send_message",
		"payload": map[string]interface{}{
			"to":                 bobID,
			"ephemeralPublicKey": ephemeralPubBase64,
			"encryptedPayload":   encryptedPayload,
			"signature":          signature,
		},
	}
	if err := aliceWsConn.WriteJSON(msgToSend); err != nil {
		return err
	}

	var incomingMsg struct {
		Type    string `json:"type"`
		Payload struct {
			From               string `json:"from"`
			EphemeralPublicKey string `json:"ephemeralPublicKey"`
			EncryptedPayload   string `json:"encryptedPayload"`
			Signature          string `json:"signature"`
		} `json:"payload"`
	}

	if err := wsConn.ReadJSON(&incomingMsg); err != nil {
		return err
	}

	if incomingMsg.Type != "new_message" {
		return fmt.Errorf("bob received unexpected message type: %s", incomingMsg.Type)
	}

	aliceServerIdentityKey, _, err := r.fetchUserPublicKey(incomingMsg.Payload.From)
	if err != nil {
		return err
	}

	if err := clientcrypto.VerifySignatureEd25519(incomingMsg.Payload.EncryptedPayload, incomingMsg.Payload.Signature, aliceServerIdentityKey); err != nil {
		return fmt.Errorf("assinatura da Alice é inválida: %w", err)
	}
	fmt.Println("✔ Assinatura digital (Ed25519) da Alice verificada com sucesso!")

	decryptedAESKey, err := clientcrypto.EcdheDeriveKeyReceiver(incomingMsg.Payload.EphemeralPublicKey, bobDhPriv)
	if err != nil {
		return fmt.Errorf("bob ecdhe failed: %w", err)
	}

	decryptedMessage, err := clientcrypto.DecryptMessageAES(incomingMsg.Payload.EncryptedPayload, decryptedAESKey)
	if err != nil {
		return err
	}

	fmt.Printf("✔ Bob leu a mensagem: %s\n", decryptedMessage)

	bobReplyText := "Tudo bem Alice! Segredo recebido e muito bem guardado."

	_, aliceServerDhKey, err := r.fetchUserPublicKey(incomingMsg.Payload.From)
	if err != nil {
		return err
	}

	bobAesKey, bobEphemeralPubBase64, err := clientcrypto.EcdheDeriveKeySender(aliceServerDhKey)
	if err != nil {
		return fmt.Errorf("bob ecdhe reply failed: %w", err)
	}

	bobEncryptedPayload, err := clientcrypto.EncryptMessageAES(bobReplyText, bobAesKey)
	if err != nil {
		return err
	}

	bobSignature, err := clientcrypto.SignMessageEd25519(bobEncryptedPayload, bobIdentityPriv)
	if err != nil {
		return err
	}

	replyMsg := map[string]interface{}{
		"type": "send_message",
		"payload": map[string]interface{}{
			"to":                 incomingMsg.Payload.From,
			"ephemeralPublicKey": bobEphemeralPubBase64,
			"encryptedPayload":   bobEncryptedPayload,
			"signature":          bobSignature,
		},
	}
	if err := wsConn.WriteJSON(replyMsg); err != nil {
		return err
	}

	var aliceIncomingMsg struct {
		Type    string `json:"type"`
		Payload struct {
			From               string `json:"from"`
			EphemeralPublicKey string `json:"ephemeralPublicKey"`
			EncryptedPayload   string `json:"encryptedPayload"`
			Signature          string `json:"signature"`
		} `json:"payload"`
	}

	if err := aliceWsConn.ReadJSON(&aliceIncomingMsg); err != nil {
		return err
	}

	if aliceIncomingMsg.Type != "new_message" {
		return fmt.Errorf("alice received unexpected message type: %s", aliceIncomingMsg.Type)
	}

	bobServerIdentityKey, _, err := r.fetchUserPublicKey(aliceIncomingMsg.Payload.From)
	if err != nil {
		return err
	}

	if err := clientcrypto.VerifySignatureEd25519(aliceIncomingMsg.Payload.EncryptedPayload, aliceIncomingMsg.Payload.Signature, bobServerIdentityKey); err != nil {
		return fmt.Errorf("assinatura do Bob é inválida: %w", err)
	}
	fmt.Println("✔ Assinatura digital (Ed25519) do Bob verificada com sucesso!")

	aliceDecryptedAESKey, err := clientcrypto.EcdheDeriveKeyReceiver(aliceIncomingMsg.Payload.EphemeralPublicKey, aliceDhPriv)
	if err != nil {
		return fmt.Errorf("alice ecdhe receive failed: %w", err)
	}

	aliceDecryptedMessage, err := clientcrypto.DecryptMessageAES(aliceIncomingMsg.Payload.EncryptedPayload, aliceDecryptedAESKey)
	if err != nil {
		return err
	}

	fmt.Printf("✔ Alice leu a mensagem: %s\n", aliceDecryptedMessage)

	return nil
}

func (r *Runner) registerUser(id, identityPubKey, dhPubKey string) (string, error) {
	payload := map[string]string{"id": id, "identityPublicKey": identityPubKey, "dhPublicKey": dhPubKey}
	body, _ := json.Marshal(payload)

	resp, err := r.Client.Post(r.BaseURL+"/keys", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}

func (r *Runner) fetchUserPublicKey(id string) (string, string, error) {
	resp, err := r.Client.Get(r.BaseURL + "/keys/" + id)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to fetch user %s", id)
	}

	var result struct {
		IdentityPublicKey string `json:"identityPublicKey"`
		DhPublicKey       string `json:"dhPublicKey"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.IdentityPublicKey, result.DhPublicKey, nil
}
