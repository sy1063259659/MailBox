package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"gptbox-server/internal/secure"
)

const (
	IntegrationRunningTTL  = 10 * time.Minute
	IntegrationHeldTTL     = 30 * time.Minute
	IntegrationSMSCooldown = 90 * time.Minute
)

var (
	ErrIntegrationResourceUnavailable = errors.New("integration resource unavailable")
	ErrIntegrationLeaseLost           = errors.New("integration lease lost")
	cardNumberPattern                 = regexp.MustCompile(`^[0-9]{12,19}$`)
	cardExpiryPattern                 = regexp.MustCompile(`^(0[1-9]|1[0-2])/([0-9]{2})$`)
	cardCVCPattern                    = regexp.MustCompile(`^[0-9]{3,4}$`)
)

type PaymentCard struct {
	ID             int64      `json:"id"`
	NumberMasked   string     `json:"numberMasked"`
	Last4          string     `json:"last4"`
	Expiry         string     `json:"expiry"`
	Status         string     `json:"status"`
	FailureReason  string     `json:"failureReason,omitempty"`
	LinkedEmails   []string   `json:"linkedEmails"`
	LeaseOwner     string     `json:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time `json:"leaseExpiresAt,omitempty"`
	UsedAt         *time.Time `json:"usedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type PaymentCardCredentials struct {
	ID     int64  `json:"id"`
	Number string `json:"number"`
	Expiry string `json:"expiry"`
	CVC    string `json:"cvc"`
	Last4  string `json:"last4"`
}

type IntegrationGroup struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	AvailableCount int    `json:"availableCount"`
}

type IntegrationLease struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Resource  string    `json:"resource"`
	OwnerID   string    `json:"ownerId"`
	QueueID   string    `json:"queueId"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type IntegrationMailbox struct {
	Email     string                  `json:"email"`
	Lease     IntegrationLease        `json:"lease"`
	HasAT     bool                    `json:"hasAccessToken"`
	Card      *PaymentCardCredentials `json:"card,omitempty"`
	CardLease *IntegrationLease       `json:"cardLease,omitempty"`
}

type IntegrationSMS struct {
	Phone string           `json:"phone"`
	Lease IntegrationLease `json:"lease"`
}

type ICloudHMEAutomationResult struct {
	MailboxEmail string `json:"email"`
	AccessToken  string `json:"accessToken,omitempty"`
	AuthFlow     string `json:"authFlow,omitempty"`
	CodexAuth    string `json:"codexAuth,omitempty"`
	Sub2API      string `json:"sub2api,omitempty"`
	Status       string `json:"status"`
	LastError    string `json:"lastError,omitempty"`
}

func (s *Store) GetICloudHMEAutomationResult(ctx context.Context, email string) (ICloudHMEAutomationResult, error) {
	var out ICloudHMEAutomationResult
	var accessTokenEncrypted, codexAuthEncrypted, sub2apiEncrypted string
	err := s.pool.QueryRow(ctx, `SELECT mailbox_email,access_token_encrypted,auth_flow,codex_auth_encrypted,sub2api_encrypted,status,last_error FROM icloud_hme_automation_results WHERE lower(mailbox_email)=$1`, normalizeEmail(email)).Scan(
		&out.MailboxEmail, &accessTokenEncrypted, &out.AuthFlow, &codexAuthEncrypted, &sub2apiEncrypted, &out.Status, &out.LastError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, errors.New("automation result not found")
	}
	if err != nil {
		return out, err
	}
	for _, item := range []struct {
		encrypted string
		target    *string
	}{{accessTokenEncrypted, &out.AccessToken}, {codexAuthEncrypted, &out.CodexAuth}, {sub2apiEncrypted, &out.Sub2API}} {
		if item.encrypted == "" {
			continue
		}
		value, decryptErr := secure.DecryptString(s.tokenKey, item.encrypted)
		if decryptErr != nil {
			return out, decryptErr
		}
		*item.target = value
	}
	return out, nil
}

type AcquireIntegrationInput struct {
	QueueID string
	GroupID int64
	Count   int
	Mode    string
}

func normalizeCardNumber(value string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(value))
}

func normalizeCardExpiry(value string) string {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return ""
	}
	month := strings.TrimSpace(parts[0])
	year := strings.TrimSpace(parts[1])
	if len(month) == 1 {
		month = "0" + month
	}
	if len(year) == 4 {
		year = year[2:]
	}
	return month + "/" + year
}

func (s *Store) cardFingerprint(number string) string {
	mac := hmac.New(sha256.New, s.tokenKey)
	_, _ = mac.Write([]byte(number))
	return hex.EncodeToString(mac.Sum(nil))
}

func newLeaseID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "lease_" + hex.EncodeToString(raw), nil
}

func maskCard(number string) string {
	if len(number) <= 4 {
		return number
	}
	return strings.Repeat("*", len(number)-4) + number[len(number)-4:]
}

func (s *Store) ImportPaymentCards(ctx context.Context, text string) ([]PaymentCard, []string, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	added := []PaymentCard{}
	parseErrors := []string{}
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "----")
		if len(parts) != 3 {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：格式必须为 卡号----MM/YY----CVC", index+1))
			continue
		}
		number := normalizeCardNumber(parts[0])
		expiry := normalizeCardExpiry(parts[1])
		cvc := strings.TrimSpace(parts[2])
		if !cardNumberPattern.MatchString(number) || !cardExpiryPattern.MatchString(expiry) || !cardCVCPattern.MatchString(cvc) {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：卡号、有效期或 CVC 格式错误", index+1))
			continue
		}
		numberEncrypted, err := secure.EncryptString(s.tokenKey, number)
		if err != nil {
			return nil, parseErrors, err
		}
		expiryEncrypted, err := secure.EncryptString(s.tokenKey, expiry)
		if err != nil {
			return nil, parseErrors, err
		}
		cvcEncrypted, err := secure.EncryptString(s.tokenKey, cvc)
		if err != nil {
			return nil, parseErrors, err
		}
		var card PaymentCard
		err = s.pool.QueryRow(ctx, `
			INSERT INTO payment_cards (number_encrypted, expiry_encrypted, cvc_encrypted, fingerprint, last4)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (fingerprint) DO NOTHING
			RETURNING id, last4, status, failure_reason, used_at, created_at, updated_at
		`, numberEncrypted, expiryEncrypted, cvcEncrypted, s.cardFingerprint(number), number[len(number)-4:]).Scan(
			&card.ID, &card.Last4, &card.Status, &card.FailureReason, &card.UsedAt, &card.CreatedAt, &card.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：卡号重复", index+1))
			continue
		}
		if err != nil {
			return nil, parseErrors, fmt.Errorf("store: import payment card: %w", err)
		}
		card.NumberMasked, card.Expiry = maskCard(number), expiry
		card.LinkedEmails = []string{}
		added = append(added, card)
	}
	return added, parseErrors, nil
}

func (s *Store) ListPaymentCards(ctx context.Context) ([]PaymentCard, error) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM integration_resource_leases WHERE expires_at <= now()`)
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.number_encrypted, c.expiry_encrypted, c.last4, c.status, c.failure_reason,
		       c.used_at, c.created_at, c.updated_at,
		       COALESCE(l.owner_id, ''), l.expires_at
		FROM payment_cards c
		LEFT JOIN integration_resource_leases l ON l.resource_type = 'card' AND l.resource_key = c.id::text
		ORDER BY c.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []PaymentCard{}
	for rows.Next() {
		var card PaymentCard
		var encryptedNumber, encryptedExpiry string
		if err := rows.Scan(&card.ID, &encryptedNumber, &encryptedExpiry, &card.Last4, &card.Status, &card.FailureReason,
			&card.UsedAt, &card.CreatedAt, &card.UpdatedAt, &card.LeaseOwner, &card.LeaseExpiresAt); err != nil {
			return nil, err
		}
		number, err := secure.DecryptString(s.tokenKey, encryptedNumber)
		if err != nil {
			return nil, err
		}
		card.Expiry, err = secure.DecryptString(s.tokenKey, encryptedExpiry)
		if err != nil {
			return nil, err
		}
		card.NumberMasked = maskCard(number)
		card.LinkedEmails = []string{}
		cards = append(cards, card)
	}
	for i := range cards {
		linkRows, err := s.pool.Query(ctx, `SELECT mailbox_email FROM icloud_hme_card_links WHERE card_id=$1 ORDER BY linked_at`, cards[i].ID)
		if err != nil {
			return nil, err
		}
		for linkRows.Next() {
			var email string
			if err := linkRows.Scan(&email); err != nil {
				linkRows.Close()
				return nil, err
			}
			cards[i].LinkedEmails = append(cards[i].LinkedEmails, email)
		}
		linkRows.Close()
	}
	return cards, rows.Err()
}

func (s *Store) SetPaymentCardStatus(ctx context.Context, id int64, status, reason string) error {
	if status != "active" && status != "disabled" {
		return errors.New("invalid card status")
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE payment_cards c SET status=$2, failure_reason=$3, used_at=CASE WHEN $2='active' THEN NULL ELSE used_at END, updated_at=now()
		WHERE c.id=$1 AND NOT EXISTS (
			SELECT 1 FROM integration_resource_leases l WHERE l.resource_type='card' AND l.resource_key=c.id::text AND l.expires_at>now()
		)
	`, id, status, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrIntegrationResourceUnavailable
	}
	return nil
}

func (s *Store) DeletePaymentCard(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM payment_cards c WHERE c.id=$1
		AND NOT EXISTS (SELECT 1 FROM icloud_hme_card_links link WHERE link.card_id=c.id)
		AND NOT EXISTS (SELECT 1 FROM integration_resource_leases l WHERE l.resource_type='card' AND l.resource_key=c.id::text AND l.expires_at>now())`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrIntegrationResourceUnavailable
	}
	return nil
}

func (s *Store) LinkPaymentCard(ctx context.Context, email string, cardID int64, source string) error {
	email = normalizeEmail(email)
	if source == "" {
		source = "manual"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lockedEmail string
	err = tx.QueryRow(ctx, `SELECT email FROM icloud_hme_aliases WHERE lower(email)=$1 AND apple_status<>'deleted' FOR UPDATE`, email).Scan(&lockedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("mailbox not found")
	}
	if err != nil {
		return err
	}
	var lockedCardID int64
	err = tx.QueryRow(ctx, `SELECT id FROM payment_cards WHERE id=$1 FOR UPDATE`, cardID).Scan(&lockedCardID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("card not found")
	}
	if err != nil {
		return err
	}
	// Lock a previously linked card too, so replacement cannot race its task acquisition.
	var previousCardID int64
	previousErr := tx.QueryRow(ctx, `SELECT card.id FROM icloud_hme_card_links link JOIN payment_cards card ON card.id=link.card_id WHERE lower(link.mailbox_email)=$1 FOR UPDATE OF card`, email).Scan(&previousCardID)
	if previousErr != nil && !errors.Is(previousErr, pgx.ErrNoRows) {
		return previousErr
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM integration_resource_leases WHERE expires_at>now() AND (
			(resource_type='email' AND lower(resource_key)=$1) OR (resource_type='card' AND resource_key=$2) OR
			(resource_type='card' AND resource_key IN (SELECT card_id::text FROM icloud_hme_card_links WHERE lower(mailbox_email)=$1))
		))`, email, fmt.Sprint(cardID)).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return ErrIntegrationResourceUnavailable
	}
	// Touch the same rows used by allocator FOR UPDATE queries. PostgreSQL will
	// re-evaluate their predicates after waiting, so a concurrent allocator sees
	// the new link instead of acting on its earlier statement snapshot.
	if _, err = tx.Exec(ctx, `UPDATE icloud_hme_aliases SET updated_at=now() WHERE lower(email)=$1`, email); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_cards SET updated_at=now() WHERE id=$1`, cardID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE icloud_hme_card_link_history SET unlinked_at=now(), end_reason='replaced' WHERE lower(mailbox_email)=$1 AND unlinked_at IS NULL`, email)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO icloud_hme_card_links(mailbox_email,card_id,source) VALUES($1,$2,$3)
		ON CONFLICT(mailbox_email) DO UPDATE SET card_id=EXCLUDED.card_id, source=EXCLUDED.source, linked_at=now()`, email, cardID, source)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO icloud_hme_card_link_history(mailbox_email,card_id,source) VALUES($1,$2,$3)`, email, cardID, source)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UnlinkPaymentCard(ctx context.Context, email, reason string) error {
	email = normalizeEmail(email)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lockedEmail string
	if err := tx.QueryRow(ctx, `SELECT email FROM icloud_hme_aliases WHERE lower(email)=$1 FOR UPDATE`, email).Scan(&lockedEmail); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("mailbox not found")
		}
		return err
	}
	var linkedCardID int64
	linkedErr := tx.QueryRow(ctx, `SELECT card.id FROM icloud_hme_card_links link JOIN payment_cards card ON card.id=link.card_id WHERE lower(link.mailbox_email)=$1 FOR UPDATE OF card`, email).Scan(&linkedCardID)
	if linkedErr != nil && !errors.Is(linkedErr, pgx.ErrNoRows) {
		return linkedErr
	}
	var leased bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM integration_resource_leases WHERE expires_at>now() AND (
		(resource_type='email' AND lower(resource_key)=$1) OR
		(resource_type='card' AND resource_key IN (SELECT card_id::text FROM icloud_hme_card_links WHERE lower(mailbox_email)=$1))
	))`, email).Scan(&leased); err != nil {
		return err
	}
	if leased {
		return ErrIntegrationResourceUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE icloud_hme_aliases SET updated_at=now() WHERE lower(email)=$1`, email); err != nil {
		return err
	}
	if linkedErr == nil {
		if _, err = tx.Exec(ctx, `UPDATE payment_cards SET updated_at=now() WHERE id=$1`, linkedCardID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE icloud_hme_card_link_history SET unlinked_at=now(), end_reason=$2 WHERE lower(mailbox_email)=$1 AND unlinked_at IS NULL`, email, reason)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM icloud_hme_card_links WHERE lower(mailbox_email)=$1`, email)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListIntegrationGroups(ctx context.Context) ([]IntegrationGroup, error) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM integration_resource_leases WHERE expires_at <= now()`)
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name, count(a.email)::int
		FROM icloud_hme_groups g
		LEFT JOIN icloud_hme_aliases a ON a.group_id=g.id AND a.active=true AND a.apple_status='active'
		 AND a.receive_key_encrypted<>''
		 AND NOT EXISTS(SELECT 1 FROM icloud_hme_card_links cl WHERE cl.mailbox_email=a.email)
		 AND NOT EXISTS(SELECT 1 FROM integration_resource_leases l WHERE l.resource_type='email' AND lower(l.resource_key)=lower(a.email))
		GROUP BY g.id,g.name ORDER BY g.sort_order,g.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []IntegrationGroup{}
	for rows.Next() {
		var item IntegrationGroup
		if err := rows.Scan(&item.ID, &item.Name, &item.AvailableCount); err != nil {
			return nil, err
		}
		groups = append(groups, item)
	}
	return groups, rows.Err()
}

func (s *Store) AcquireIntegrationResources(ctx context.Context, input AcquireIntegrationInput) ([]IntegrationMailbox, error) {
	input.QueueID = strings.TrimSpace(input.QueueID)
	if input.QueueID == "" || input.GroupID <= 0 || input.Count <= 0 || input.Count > 100 {
		return nil, errors.New("invalid acquire request")
	}
	if input.Mode != "auth" && input.Mode != "payment" && input.Mode != "sync_payment" {
		return nil, errors.New("invalid acquire mode")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	_, _ = tx.Exec(ctx, `DELETE FROM integration_resource_leases WHERE expires_at<=now()`)
	rows, err := tx.Query(ctx, `
		SELECT a.email, COALESCE(r.access_token_encrypted,'')<>''
		FROM icloud_hme_aliases a
		LEFT JOIN icloud_hme_automation_results r ON r.mailbox_email=a.email
		WHERE a.group_id=$1 AND a.active=true AND a.apple_status='active' AND a.receive_key_encrypted<>''
		  AND NOT EXISTS(SELECT 1 FROM icloud_hme_card_links cl WHERE cl.mailbox_email=a.email)
		  AND NOT EXISTS(SELECT 1 FROM integration_resource_leases l WHERE l.resource_type='email' AND lower(l.resource_key)=lower(a.email))
		  AND ($3<>'auth' OR COALESCE(r.access_token_encrypted,'')='')
		ORDER BY a.import_order ASC, a.created_at ASC, a.email ASC
		LIMIT $2 FOR UPDATE OF a SKIP LOCKED
	`, input.GroupID, input.Count, input.Mode)
	if err != nil {
		return nil, err
	}
	type pickedEmail struct {
		email string
		hasAT bool
	}
	picked := []pickedEmail{}
	for rows.Next() {
		var p pickedEmail
		if err := rows.Scan(&p.email, &p.hasAT); err != nil {
			rows.Close()
			return nil, err
		}
		picked = append(picked, p)
	}
	rows.Close()
	if len(picked) != input.Count {
		return nil, fmt.Errorf("%w: mailbox count %d/%d", ErrIntegrationResourceUnavailable, len(picked), input.Count)
	}
	for _, item := range picked {
		var stillAvailable bool
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM icloud_hme_card_links WHERE lower(mailbox_email)=lower($1)) AND NOT EXISTS(SELECT 1 FROM integration_resource_leases WHERE resource_type='email' AND lower(resource_key)=lower($1))`, item.email).Scan(&stillAvailable); err != nil {
			return nil, err
		}
		if !stillAvailable {
			return nil, fmt.Errorf("%w: mailbox changed during allocation", ErrIntegrationResourceUnavailable)
		}
	}
	expires := time.Now().Add(IntegrationRunningTTL)
	result := make([]IntegrationMailbox, 0, len(picked))
	for _, p := range picked {
		id, err := newLeaseID()
		if err != nil {
			return nil, err
		}
		owner := input.QueueID + "/" + p.email
		_, err = tx.Exec(ctx, `INSERT INTO integration_resource_leases(id,resource_type,resource_key,owner_id,queue_id,expires_at) VALUES($1,'email',$2,$3,$4,$5)`, id, p.email, owner, input.QueueID, expires)
		if err != nil {
			return nil, err
		}
		result = append(result, IntegrationMailbox{Email: p.email, HasAT: p.hasAT, Lease: IntegrationLease{ID: id, Type: "email", Resource: p.email, OwnerID: owner, QueueID: input.QueueID, State: "running", ExpiresAt: expires}})
	}
	cardCount := 0
	if input.Mode == "payment" {
		cardCount = input.Count
	}
	if input.Mode == "sync_payment" {
		cardCount = 1
	}
	if cardCount > 0 {
		cardRows, err := tx.Query(ctx, `SELECT id,last4 FROM payment_cards c WHERE c.status='active' AND NOT EXISTS(SELECT 1 FROM icloud_hme_card_links cl WHERE cl.card_id=c.id) AND NOT EXISTS(SELECT 1 FROM integration_resource_leases l WHERE l.resource_type='card' AND l.resource_key=c.id::text) ORDER BY c.id ASC LIMIT $1 FOR UPDATE OF c SKIP LOCKED`, cardCount)
		if err != nil {
			return nil, err
		}
		type pickedCard struct {
			id    int64
			last4 string
		}
		cards := []pickedCard{}
		for cardRows.Next() {
			var c pickedCard
			if err := cardRows.Scan(&c.id, &c.last4); err != nil {
				cardRows.Close()
				return nil, err
			}
			cards = append(cards, c)
		}
		cardRows.Close()
		if len(cards) != cardCount {
			return nil, fmt.Errorf("%w: card count %d/%d", ErrIntegrationResourceUnavailable, len(cards), cardCount)
		}
		for i, c := range cards {
			var stillAvailable bool
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM icloud_hme_card_links WHERE card_id=$1) AND NOT EXISTS(SELECT 1 FROM integration_resource_leases WHERE resource_type='card' AND resource_key=$1::text)`, c.id).Scan(&stillAvailable); err != nil {
				return nil, err
			}
			if !stillAvailable {
				return nil, fmt.Errorf("%w: card changed during allocation", ErrIntegrationResourceUnavailable)
			}
			id, err := newLeaseID()
			if err != nil {
				return nil, err
			}
			owner := input.QueueID + "/" + picked[i].email
			if input.Mode == "sync_payment" {
				owner = input.QueueID + "/shared-card"
			}
			_, err = tx.Exec(ctx, `INSERT INTO integration_resource_leases(id,resource_type,resource_key,owner_id,queue_id,expires_at) VALUES($1,'card',$2,$3,$4,$5)`, id, fmt.Sprint(c.id), owner, input.QueueID, expires)
			if err != nil {
				return nil, err
			}
			lease := IntegrationLease{ID: id, Type: "card", Resource: fmt.Sprint(c.id), OwnerID: owner, QueueID: input.QueueID, State: "running", ExpiresAt: expires}
			credentials := &PaymentCardCredentials{ID: c.id, Last4: c.last4}
			if input.Mode == "sync_payment" {
				for j := range result {
					result[j].Card = credentials
					result[j].CardLease = &lease
				}
			} else {
				result[i].Card = credentials
				result[i].CardLease = &lease
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) UpdateIntegrationQueueLease(ctx context.Context, queueID, action string) (time.Time, error) {
	var state string
	var ttl time.Duration
	switch action {
	case "heartbeat", "resume":
		state = "running"
		ttl = IntegrationRunningTTL
	case "hold":
		state = "held"
		ttl = IntegrationHeldTTL
	default:
		return time.Time{}, errors.New("invalid lease action")
	}
	expires := time.Now().Add(ttl)
	result, err := s.pool.Exec(ctx, `UPDATE integration_resource_leases SET state=$2,expires_at=$3,updated_at=now() WHERE queue_id=$1 AND expires_at>now()`, queueID, state, expires)
	if err != nil {
		return time.Time{}, err
	}
	if result.RowsAffected() == 0 {
		return time.Time{}, ErrIntegrationLeaseLost
	}
	return expires, nil
}

func (s *Store) ReleaseIntegrationQueue(ctx context.Context, queueID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM integration_resource_leases WHERE queue_id=$1`, strings.TrimSpace(queueID))
	return err
}

func (s *Store) ListIntegrationLeases(ctx context.Context) ([]IntegrationLease, error) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM integration_resource_leases WHERE expires_at<=now()`)
	rows, err := s.pool.Query(ctx, `SELECT id,resource_type,resource_key,owner_id,queue_id,state,expires_at FROM integration_resource_leases ORDER BY expires_at ASC,id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []IntegrationLease{}
	for rows.Next() {
		var item IntegrationLease
		if err := rows.Scan(&item.ID, &item.Type, &item.Resource, &item.OwnerID, &item.QueueID, &item.State, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ForceReleaseIntegrationLease(ctx context.Context, leaseID string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM integration_resource_leases WHERE id=$1`, strings.TrimSpace(leaseID))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrIntegrationLeaseLost
	}
	return nil
}

func (s *Store) SaveIntegrationAuthResult(ctx context.Context, leaseID, accessToken, authFlow, codexAuth, sub2api, status, lastError string) error {
	if status == "" {
		status = "authenticated"
	}
	var email string
	if err := s.pool.QueryRow(ctx, `SELECT resource_key FROM integration_resource_leases WHERE id=$1 AND resource_type='email' AND expires_at>now()`, leaseID).Scan(&email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIntegrationLeaseLost
		}
		return err
	}
	encrypt := func(value string) (string, error) {
		if strings.TrimSpace(value) == "" {
			return "", nil
		}
		return secure.EncryptString(s.tokenKey, value)
	}
	at, err := encrypt(accessToken)
	if err != nil {
		return err
	}
	auth, err := encrypt(codexAuth)
	if err != nil {
		return err
	}
	sub, err := encrypt(sub2api)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO icloud_hme_automation_results(mailbox_email,status,access_token_encrypted,auth_flow,codex_auth_encrypted,sub2api_encrypted,last_error) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(mailbox_email) DO UPDATE SET status=EXCLUDED.status,access_token_encrypted=CASE WHEN EXCLUDED.access_token_encrypted='' THEN icloud_hme_automation_results.access_token_encrypted ELSE EXCLUDED.access_token_encrypted END,auth_flow=CASE WHEN EXCLUDED.auth_flow='' THEN icloud_hme_automation_results.auth_flow ELSE EXCLUDED.auth_flow END,codex_auth_encrypted=CASE WHEN EXCLUDED.codex_auth_encrypted='' THEN icloud_hme_automation_results.codex_auth_encrypted ELSE EXCLUDED.codex_auth_encrypted END,sub2api_encrypted=CASE WHEN EXCLUDED.sub2api_encrypted='' THEN icloud_hme_automation_results.sub2api_encrypted ELSE EXCLUDED.sub2api_encrypted END,last_error=EXCLUDED.last_error,updated_at=now()`, email, status, at, authFlow, auth, sub, lastError)
	return err
}

func (s *Store) GetIntegrationMailCredentials(ctx context.Context, leaseID string) (ICloudHMEMailCredentials, error) {
	var credentials ICloudHMEMailCredentials
	var encryptedPassword string
	err := s.pool.QueryRow(ctx, `
		SELECT alias.email, source.icloud_email, source.app_password_encrypted
		FROM integration_resource_leases lease
		JOIN icloud_hme_aliases alias ON lower(alias.email)=lower(lease.resource_key)
		JOIN icloud_hme_source_accounts source ON source.id=alias.source_account_id
		WHERE lease.id=$1 AND lease.resource_type='email' AND lease.expires_at>now()
		  AND alias.active=true AND alias.apple_status='active'
	`, strings.TrimSpace(leaseID)).Scan(&credentials.AliasEmail, &credentials.ICloudEmail, &encryptedPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return credentials, ErrIntegrationLeaseLost
	}
	if err != nil {
		return credentials, err
	}
	credentials.AppPassword, err = secure.DecryptString(s.tokenKey, encryptedPassword)
	if err != nil {
		return ICloudHMEMailCredentials{}, err
	}
	return credentials, nil
}

func (s *Store) GetIntegrationCardCredentials(ctx context.Context, leaseID string) (PaymentCardCredentials, error) {
	var out PaymentCardCredentials
	var numberEncrypted, expiryEncrypted, cvcEncrypted string
	err := s.pool.QueryRow(ctx, `SELECT card.id,card.number_encrypted,card.expiry_encrypted,card.cvc_encrypted,card.last4 FROM integration_resource_leases lease JOIN payment_cards card ON card.id::text=lease.resource_key WHERE lease.id=$1 AND lease.resource_type='card' AND lease.expires_at>now()`, strings.TrimSpace(leaseID)).Scan(
		&out.ID, &numberEncrypted, &expiryEncrypted, &cvcEncrypted, &out.Last4,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrIntegrationLeaseLost
	}
	if err != nil {
		return out, err
	}
	if out.Number, err = secure.DecryptString(s.tokenKey, numberEncrypted); err != nil {
		return PaymentCardCredentials{}, err
	}
	if out.Expiry, err = secure.DecryptString(s.tokenKey, expiryEncrypted); err != nil {
		return PaymentCardCredentials{}, err
	}
	if out.CVC, err = secure.DecryptString(s.tokenKey, cvcEncrypted); err != nil {
		return PaymentCardCredentials{}, err
	}
	return out, nil
}

func (s *Store) AcquireIntegrationSMS(ctx context.Context, emailLeaseID, ownerID string) (IntegrationSMS, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return IntegrationSMS{}, errors.New("SMS lease owner required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return IntegrationSMS{}, err
	}
	defer tx.Rollback(ctx)
	_, _ = tx.Exec(ctx, `DELETE FROM integration_resource_leases WHERE expires_at<=now()`)
	var email, queueID string
	if err := tx.QueryRow(ctx, `SELECT resource_key,queue_id FROM integration_resource_leases WHERE id=$1 AND resource_type='email' AND expires_at>now() FOR UPDATE`, emailLeaseID).Scan(&email, &queueID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IntegrationSMS{}, ErrIntegrationLeaseLost
		}
		return IntegrationSMS{}, err
	}
	var smsID int64
	var phone string
	err = tx.QueryRow(ctx, `
		SELECT account.id,account.phone FROM sms_account_bindings binding
		JOIN sms_accounts account ON account.id=binding.sms_account_id AND account.status='active'
		WHERE lower(binding.mailbox_email)=lower($1)
		ORDER BY account.id ASC LIMIT 1 FOR UPDATE OF account SKIP LOCKED
	`, email).Scan(&smsID, &phone)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT account.id,account.phone FROM sms_accounts account
			WHERE account.status='active'
			  AND NOT EXISTS(SELECT 1 FROM integration_resource_leases lease WHERE lease.resource_type='sms' AND lease.resource_key=account.id::text)
			  AND (SELECT count(*) FROM sms_account_bindings binding WHERE binding.sms_account_id=account.id) < 3
			  AND COALESCE((SELECT max(history.bound_at) FROM sms_account_binding_history history WHERE history.sms_account_id=account.id),'-infinity'::timestamptz) <= now()-interval '90 minutes'
			ORDER BY account.id ASC LIMIT 1 FOR UPDATE OF account SKIP LOCKED
		`).Scan(&smsID, &phone)
		if errors.Is(err, pgx.ErrNoRows) {
			return IntegrationSMS{}, fmt.Errorf("%w: SMS number", ErrIntegrationResourceUnavailable)
		}
		if err != nil {
			return IntegrationSMS{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO sms_account_bindings(sms_account_id,mailbox_email) VALUES($1,$2)`, smsID, email)
		if err != nil {
			return IntegrationSMS{}, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO sms_account_binding_history(sms_account_id,mailbox_email) VALUES($1,$2)`, smsID, email)
		if err != nil {
			return IntegrationSMS{}, err
		}
	} else if err != nil {
		return IntegrationSMS{}, err
	} else {
		var busy bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM integration_resource_leases WHERE resource_type='sms' AND resource_key=$1 AND expires_at>now())`, fmt.Sprint(smsID)).Scan(&busy); err != nil {
			return IntegrationSMS{}, err
		}
		if busy {
			return IntegrationSMS{}, fmt.Errorf("%w: bound SMS number is in use", ErrIntegrationResourceUnavailable)
		}
	}
	id, err := newLeaseID()
	if err != nil {
		return IntegrationSMS{}, err
	}
	expires := time.Now().Add(IntegrationRunningTTL)
	_, err = tx.Exec(ctx, `INSERT INTO integration_resource_leases(id,resource_type,resource_key,owner_id,queue_id,expires_at) VALUES($1,'sms',$2,$3,$4,$5)`, id, fmt.Sprint(smsID), ownerID, queueID, expires)
	if err != nil {
		return IntegrationSMS{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IntegrationSMS{}, err
	}
	return IntegrationSMS{Phone: phone, Lease: IntegrationLease{ID: id, Type: "sms", Resource: fmt.Sprint(smsID), OwnerID: ownerID, QueueID: queueID, State: "running", ExpiresAt: expires}}, nil
}

func (s *Store) GetIntegrationSMSReceiveURL(ctx context.Context, leaseID string) (string, error) {
	var encrypted string
	err := s.pool.QueryRow(ctx, `SELECT account.receive_url_encrypted FROM integration_resource_leases lease JOIN sms_accounts account ON account.id::text=lease.resource_key WHERE lease.id=$1 AND lease.resource_type='sms' AND lease.expires_at>now() AND account.status='active'`, leaseID).Scan(&encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrIntegrationLeaseLost
	}
	if err != nil {
		return "", err
	}
	return secure.DecryptString(s.tokenKey, encrypted)
}

func (s *Store) CompleteIntegrationPayment(ctx context.Context, queueID string, emails []string, cardLeaseID string) error {
	normalized := uniqueNormalizedEmails(emails)
	if len(normalized) == 0 {
		return errors.New("successful emails required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var cardID int64
	var cardOwner string
	if err := tx.QueryRow(ctx, `SELECT resource_key::bigint,owner_id FROM integration_resource_leases WHERE id=$1 AND queue_id=$2 AND resource_type='card' AND expires_at>now() FOR UPDATE`, cardLeaseID, queueID).Scan(&cardID, &cardOwner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIntegrationLeaseLost
		}
		return err
	}
	for _, email := range normalized {
		if cardOwner != queueID+"/shared-card" && cardOwner != queueID+"/"+email {
			return errors.New("card lease is not paired with mailbox")
		}
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM integration_resource_leases WHERE queue_id=$1 AND resource_type='email' AND lower(resource_key)=$2 AND expires_at>now())`, queueID, email).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrIntegrationLeaseLost
		}
		linked, err := tx.Exec(ctx, `INSERT INTO icloud_hme_card_links(mailbox_email,card_id,source) VALUES($1,$2,'task') ON CONFLICT(mailbox_email) DO NOTHING`, email, cardID)
		if err != nil {
			return err
		}
		if linked.RowsAffected() > 0 {
			_, err = tx.Exec(ctx, `INSERT INTO icloud_hme_card_link_history(mailbox_email,card_id,source) VALUES($1,$2,'task')`, email, cardID)
			if err != nil {
				return err
			}
		} else {
			var existingCardID int64
			if err := tx.QueryRow(ctx, `SELECT card_id FROM icloud_hme_card_links WHERE lower(mailbox_email)=$1`, email).Scan(&existingCardID); err != nil {
				return err
			}
			if existingCardID != cardID {
				return errors.New("mailbox is already linked to another card")
			}
		}
	}
	_, err = tx.Exec(ctx, `UPDATE payment_cards SET status='used',used_at=now(),failure_reason='',updated_at=now() WHERE id=$1`, cardID)
	if err != nil {
		return err
	}
	// Keep both leases until queue release. This makes result submission
	// idempotent across lost HTTP responses and protects manual association
	// while synchronized accounts finish one by one.
	return tx.Commit(ctx)
}
