package imapmail

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
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

	client, err := c.connectICloudPassword(ctx, email, password)
	if err != nil {
		return MessageDetail{}, err
	}
	defer client.Close()

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

func (c Client) ListMessagesByRecipient(ctx context.Context, email, password, recipient string, limit int, cursor string) (ListResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	client, err := c.connectICloudPassword(ctx, email, password)
	if err != nil {
		return ListResult{}, err
	}
	defer client.Close()

	criteria := imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: strings.ToLower(strings.TrimSpace(recipient))}},
	}
	if strings.TrimSpace(cursor) != "" {
		cursorUID, err := parseUID(cursor)
		if err != nil {
			return ListResult{}, err
		}
		if cursorUID == 0 {
			return ListResult{}, nil
		}
		criteria.UID = []imap.UIDSet{uidRange(1, cursorUID)}
	}
	data, err := client.UIDSearch(&criteria, nil).Wait()
	if err != nil {
		return ListResult{}, fmt.Errorf("imapmail: search iCloud recipient: %w", err)
	}
	uids := data.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	if len(uids) > limit {
		uids = uids[:limit]
	}
	if len(uids) == 0 {
		return ListResult{Messages: []MessageSummary{}}, nil
	}
	messages, err := client.Fetch(uidSetFromUIDs(uids), &imap.FetchOptions{
		UID: true, Envelope: true, Flags: true, InternalDate: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return ListResult{}, fmt.Errorf("imapmail: fetch iCloud message list: %w", err)
	}
	summaries := make([]MessageSummary, 0, len(messages))
	for _, message := range messages {
		summaries = append(summaries, summaryFromFetch(message))
	}
	sort.Slice(summaries, func(i, j int) bool {
		left, _ := strconv.ParseUint(summaries[i].ID, 10, 32)
		right, _ := strconv.ParseUint(summaries[j].ID, 10, 32)
		return left > right
	})
	nextCursor := ""
	if len(summaries) == limit {
		last, _ := strconv.ParseUint(summaries[len(summaries)-1].ID, 10, 32)
		if last > 1 {
			nextCursor = strconv.FormatUint(last-1, 10)
		}
	}
	return ListResult{Messages: summaries, NextCursor: nextCursor}, nil
}

func (c Client) GetMessageByRecipient(ctx context.Context, email, password, recipient, uid string) (MessageDetail, error) {
	parsedUID, err := parseUID(uid)
	if err != nil {
		return MessageDetail{}, err
	}
	client, err := c.connectICloudPassword(ctx, email, password)
	if err != nil {
		return MessageDetail{}, err
	}
	defer client.Close()

	data, err := client.UIDSearch(&imap.SearchCriteria{
		UID:    []imap.UIDSet{imap.UIDSetNum(parsedUID)},
		Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: strings.ToLower(strings.TrimSpace(recipient))}},
	}, nil).Wait()
	if err != nil {
		return MessageDetail{}, fmt.Errorf("imapmail: verify iCloud recipient: %w", err)
	}
	if len(data.AllUIDs()) == 0 {
		return MessageDetail{}, errors.New("icloud_mail_not_found")
	}
	return fetchICloudMessage(client, parsedUID)
}

// ListMessageDetailsByRecipient fetches a page of complete messages using one
// authenticated IMAP session. The recipient search prevents cross-alias reads.
func (c Client) ListMessageDetailsByRecipient(ctx context.Context, email, password, recipient string, limit int, cursor string) (DetailListResult, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	client, err := c.connectICloudPassword(ctx, email, password)
	if err != nil {
		return DetailListResult{}, err
	}
	defer client.Close()

	criteria := imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: strings.ToLower(strings.TrimSpace(recipient))}},
	}
	if strings.TrimSpace(cursor) != "" {
		cursorUID, err := parseUID(cursor)
		if err != nil {
			return DetailListResult{}, err
		}
		if cursorUID == 0 {
			return DetailListResult{Messages: []MessageDetail{}}, nil
		}
		criteria.UID = []imap.UIDSet{uidRange(1, cursorUID)}
	}
	data, err := client.UIDSearch(&criteria, nil).Wait()
	if err != nil {
		return DetailListResult{}, fmt.Errorf("imapmail: search iCloud recipient details: %w", err)
	}
	uids := data.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	if len(uids) > limit {
		uids = uids[:limit]
	}
	if len(uids) == 0 {
		return DetailListResult{Messages: []MessageDetail{}}, nil
	}

	section := &imap.FetchItemBodySection{Peek: true}
	fetched, err := client.Fetch(uidSetFromUIDs(uids), &imap.FetchOptions{
		UID: true, Envelope: true, Flags: true, InternalDate: true,
		BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return DetailListResult{}, fmt.Errorf("imapmail: fetch iCloud message details: %w", err)
	}
	messages := make([]MessageDetail, 0, len(fetched))
	for _, item := range fetched {
		parsed := parseMessageBody(item.FindBodySection(section))
		detail := detailFromFetch(item)
		if parsed.HTML != "" {
			detail.ContentType = "text/html"
			detail.Content = parsed.HTML
		} else {
			detail.ContentType = "text/plain"
			detail.Content = parsed.Text
		}
		messages = append(messages, detail)
	}
	sort.Slice(messages, func(i, j int) bool {
		left, _ := strconv.ParseUint(messages[i].ID, 10, 32)
		right, _ := strconv.ParseUint(messages[j].ID, 10, 32)
		return left > right
	})
	nextCursor := ""
	if len(messages) == limit {
		last, _ := strconv.ParseUint(messages[len(messages)-1].ID, 10, 32)
		if last > 1 {
			nextCursor = strconv.FormatUint(last-1, 10)
		}
	}
	return DetailListResult{Messages: messages, NextCursor: nextCursor}, nil
}

func (c Client) connectICloudPassword(ctx context.Context, email, password string) (*imapclient.Client, error) {
	email = strings.TrimSpace(email)
	password = normalizeICloudAppPassword(password)
	if email == "" || password == "" {
		return nil, errors.New("imapmail: iCloud email and app password are required")
	}
	dial := c.Dial
	if dial == nil {
		dial = defaultDial
	}
	address := c.Address
	if address == "" || address == defaultAddress {
		address = defaultICloudAddress
	}

	var loginErr error
	for _, username := range iCloudLoginUsernames(email) {
		client, err := dial(ctx, address)
		if err != nil {
			return nil, fmt.Errorf("imapmail: dial iCloud: %w", err)
		}
		if err := client.Login(username, password).Wait(); err != nil {
			loginErr = err
			client.Close()
			continue
		}
		if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			client.Close()
			return nil, fmt.Errorf("imapmail: select iCloud Inbox: %w", err)
		}
		return client, nil
	}
	if loginErr != nil {
		return nil, fmt.Errorf("imapmail: iCloud login: %w", loginErr)
	}
	return nil, errors.New("imapmail: iCloud login username is required")
}

func (c Client) ValidateICloudPassword(ctx context.Context, email, password string) error {
	client, err := c.connectICloudPassword(ctx, email, password)
	if err != nil {
		return err
	}
	return client.Close()
}

func normalizeICloudAppPassword(password string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) {
			return -1
		}
		return char
	}, password)
}

func iCloudLoginUsernames(email string) []string {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	local, _, hasDomain := strings.Cut(email, "@")
	local = strings.TrimSpace(local)
	if hasDomain && local != "" && !strings.EqualFold(local, email) {
		return []string{local, email}
	}
	return []string{email}
}

func fetchICloudMessage(client *imapclient.Client, uid imap.UID) (MessageDetail, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	messages, err := client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
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
