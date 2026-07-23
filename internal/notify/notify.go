// Package notify sends plain-text alerts to Telegram (and always to stdout).
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Notifier delivers alerts. A zero token/chatID prints only.
type Notifier struct {
	Token  string
	ChatID string
	client *http.Client
}

func New(token, chatID string) *Notifier {
	return &Notifier{Token: token, ChatID: chatID, client: &http.Client{Timeout: 10 * time.Second}}
}

// Send prints the message and, if configured, delivers it to Telegram.
func (n *Notifier) Send(text string) {
	fmt.Println(text)
	if n.Token == "" || n.ChatID == "" {
		return
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.Token)
	body, _ := json.Marshal(map[string]string{"chat_id": n.ChatID, "text": text})
	resp, err := n.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Println("[ERROR] Telegram:", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[ERROR] Telegram: %d\n", resp.StatusCode)
	}
}
