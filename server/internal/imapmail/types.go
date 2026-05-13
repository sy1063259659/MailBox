package imapmail

import "time"

type FolderMap map[string]string

type Client struct {
	Address string
	Dial    DialFunc
}

type MessageSummary struct {
	ID             string    `json:"id"`
	Subject        string    `json:"subject"`
	From           []Address `json:"from"`
	To             []Address `json:"to"`
	Cc             []Address `json:"cc"`
	ReceivedAt     time.Time `json:"receivedAt"`
	Preview        string    `json:"preview"`
	IsRead         bool      `json:"isRead"`
	HasAttachments bool      `json:"hasAttachments"`
}

type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

type ListResult struct {
	Messages   []MessageSummary `json:"messages"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type MessageDetail struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	From        []Address `json:"from"`
	To          []Address `json:"to"`
	Cc          []Address `json:"cc"`
	ReceivedAt  time.Time `json:"receivedAt"`
	IsRead      bool      `json:"isRead"`
	ContentType string    `json:"contentType"`
	Content     string    `json:"content"`
}
