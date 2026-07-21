package icloudhme

import "testing"

func TestParseAliasList(t *testing.T) {
	aliases := parseAliasList(`{"result":{"hmeEmails":[{"hme":"One@icloud.com","anonymousId":"a1","label":"Primary","state":"active"},{"email":"off@icloud.com","state":"inactive"}]}}`)
	if len(aliases) != 2 {
		t.Fatalf("len = %d, want 2", len(aliases))
	}
	if aliases[0].Email != "one@icloud.com" || aliases[0].AnonymousID != "a1" || !aliases[0].Active {
		t.Fatalf("first = %#v", aliases[0])
	}
	if aliases[1].Active {
		t.Fatalf("inactive alias = %#v", aliases[1])
	}
}

func TestParseAliasListRejectsInvalidJSON(t *testing.T) {
	if aliases := parseAliasList(`not-json`); len(aliases) != 0 {
		t.Fatalf("aliases = %#v", aliases)
	}
}
