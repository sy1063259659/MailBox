package imapmail

import (
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func listFolders(client *imapclient.Client) (FolderMap, error) {
	boxes, err := client.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("imapmail: list folders: %w", err)
	}

	folders := FolderMap{"inbox": "INBOX"}
	var junkFallback string
	for _, box := range boxes {
		if box == nil || box.Mailbox == "" {
			continue
		}
		lower := strings.ToLower(box.Mailbox)
		if lower == "inbox" {
			folders["inbox"] = box.Mailbox
		}
		for _, attr := range box.Attrs {
			if attr == imap.MailboxAttrJunk {
				folders["junkemail"] = box.Mailbox
			}
		}
		if junkFallback == "" && isJunkFallbackName(box.Mailbox) {
			junkFallback = box.Mailbox
		}
	}
	if folders["junkemail"] == "" && junkFallback != "" {
		folders["junkemail"] = junkFallback
	}
	return folders, nil
}

func resolveFolder(folder string, folders FolderMap) (string, error) {
	key := strings.ToLower(strings.TrimSpace(folder))
	if key == "" {
		key = "inbox"
	}
	if mapped := folders[key]; mapped != "" {
		return mapped, nil
	}
	if key == "inbox" {
		return "INBOX", nil
	}
	return "", fmt.Errorf("imapmail: folder %q not found", folder)
}

func isJunkFallbackName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "junk", "junk email", "junk e-mail", "垃圾邮件":
		return true
	default:
		return false
	}
}
