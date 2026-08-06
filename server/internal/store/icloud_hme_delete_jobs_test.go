package store

import (
	"fmt"
	"testing"
)

func TestNormalizeICloudHMEDeleteEmails(t *testing.T) {
	emails, err := normalizeICloudHMEDeleteEmails([]string{
		" One@iCloud.com ",
		"one@icloud.com",
		"two@icloud.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 2 || emails[0] != "one@icloud.com" || emails[1] != "two@icloud.com" {
		t.Fatalf("emails = %#v", emails)
	}
}

func TestNormalizeICloudHMEDeleteEmailsRejectsInvalidCounts(t *testing.T) {
	if _, err := normalizeICloudHMEDeleteEmails(nil); err == nil {
		t.Fatal("empty delete job should fail")
	}
	emails := make([]string, maxICloudHMEDeleteJobItems+1)
	for index := range emails {
		emails[index] = fmt.Sprintf("alias-%d@icloud.com", index)
	}
	if _, err := normalizeICloudHMEDeleteEmails(emails); err == nil {
		t.Fatal("oversized delete job should fail")
	}
}
