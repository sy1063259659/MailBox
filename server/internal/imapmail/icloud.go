package imapmail

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-imap/v2"
)

const defaultICloudAddress = "imap.mail.me.com:993"

// GetLatestMessageByRecipient logs in with an iCloud app-specific password and
// returns the newest Inbox message addressed to the Hide My Email alias.
func (c Client) GetLatestMessageByRecipient(ctx context.Context, email, password, recipient string) (MessageDetail, error) {
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if email == "" || password == "" || recipient == "" {
		return MessageDetail{}, errors.New("imapmail: iCloud email, app password and recipient are required")
	}

	dial := c.Dial
	if dial == nil {
		dial = defaultDial
	}
	address := c.Address
	if address == "" || address == defaultAddress {
		address = defaultICloudAddress
	}
	client, err := dial(ctx, address)
	if err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: dial iCloud: %w", err)
	}
	defer client.Close()

	if err := client.Login(email, password).Wait(); err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: iCloud login: %w", err)
	}
	if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: select iCloud Inbox: %w", err)
	}

	data, err := client.UIDSearch(&imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: recipient}},
	}, nil).Wait()
	if err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: search iCloud recipient: %w", err)
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return MessageDetail{}, errors.New("icloud_mail_not_found")
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })

	section := &imap.FetchItemBodySection{Peek: true}
	messages, err := client.Fetch(imap.UIDSetNum(uids[0]), &imap.FetchOptions{
		UID: true, Envelope: true, Flags: true, InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: fetch iCloud message: %w", err)
	}
	if len(messages) == 0 {
		return MessageDetail{}, errors.New("icloud_mail_not_found")
	}

	parsed := parseMessageBody(messages[0].FindBodySection(section))
	detail := detailFromFetch(messages[0])
	if parsed.HTML != "" {
		detail.ContentType = "text/html"
		detail.Content = parsed.HTML
	} else {
		detail.ContentType = "text/plain"
		detail.Content = parsed.Text
	}
	return detail, nil
}
