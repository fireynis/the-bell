package postgres

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateGraphDepth(t *testing.T) {
	tests := []struct {
		name    string
		depth   int
		wantErr bool
	}{
		{"minimum valid depth", 1, false},
		{"typical propagation depth", 3, false},
		{"maximum allowed", maxGraphDepth, false},
		{"one past the maximum", maxGraphDepth + 1, true},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGraphDepth("depth", tt.depth)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateGraphDepth(%d) error = %v, wantErr %v", tt.depth, err, tt.wantErr)
			}
		})
	}
}

// The parameter name is included so the caller can tell which argument was
// wrong; FindVouchersWithDepth calls its argument maxDepth, not depth.
func TestValidateGraphDepth_NamesTheOffendingParameter(t *testing.T) {
	err := validateGraphDepth("maxDepth", 0)
	if err == nil || !strings.Contains(err.Error(), "maxDepth") {
		t.Errorf("error = %v, want it to name maxDepth", err)
	}
}

func TestParseAgtypeString(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"quoted uuid", `"0195f3a0-1234-7000-8000-000000000001"`, "0195f3a0-1234-7000-8000-000000000001"},
		{"quoted simple value", `"user-123"`, "user-123"},
		{"empty quoted string", `""`, ""},
		{"unquoted value is passed through", `user-123`, "user-123"},
		{"empty input", ``, ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAgtypeString(tt.raw); got != tt.want {
				t.Errorf("parseAgtypeString(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// Decoding rather than trimming matters for any value containing a quote or a
// backslash: trimming would strip real characters and leave escapes undecoded.
func TestParseAgtypeString_DecodesEscapes(t *testing.T) {
	tests := []string{
		`a"quoted"name`,
		`back\slash`,
		`tab	separated`,
		`"leading and trailing quotes"`,
		`unicode: café`,
	}

	for _, original := range tests {
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshaling %q: %v", original, err)
		}

		if got := parseAgtypeString(string(encoded)); got != original {
			t.Errorf("round trip of %q gave %q", original, got)
		}
	}
}

func TestParseAgtypeInt(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{"single hop", "1", 1, false},
		{"multiple hops", "3", 3, false},
		{"zero", "0", 0, false},
		{"surrounding whitespace", "  2  ", 2, false},
		{"newline suffix", "4\n", 4, false},
		{"not a number", "abc", 0, true},
		{"empty", "", 0, true},
		{"quoted number is not an integer", `"3"`, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAgtypeInt(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAgtypeInt(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("parseAgtypeInt(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

// A malformed depth must surface as an error rather than silently becoming 0,
// which would record a propagated penalty at the direct-penalty depth.
func TestParseAgtypeInt_DoesNotSilentlyReturnZero(t *testing.T) {
	got, err := parseAgtypeInt("not-a-depth")
	if err == nil {
		t.Fatalf("expected an error, got depth %d", got)
	}
}

func TestParseAgtypeBool(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"  true  ", true},
		{"true\n", true},
		{"false", false},
		{"", false},
		{"TRUE", false},
		{"1", false},
		{"null", false},
		{"garbage", false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := parseAgtypeBool(tt.raw); got != tt.want {
				t.Errorf("parseAgtypeBool(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// The cycle check fails closed: an unrecognized result reports no cycle, so a
// glitch blocks nobody from vouching rather than blocking everybody.
func TestParseAgtypeBool_FailsClosed(t *testing.T) {
	for _, raw := range []string{"", "null", "ERROR", "TRUE", "yes"} {
		if parseAgtypeBool(raw) {
			t.Errorf("parseAgtypeBool(%q) reported a cycle", raw)
		}
	}
}

func TestCypherParams(t *testing.T) {
	got, err := cypherParams("user_id", "u1", "depth", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if decoded["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", decoded["user_id"])
	}
	if decoded["depth"] != float64(3) {
		t.Errorf("depth = %v, want 3", decoded["depth"])
	}
}

func TestCypherParams_Rejects(t *testing.T) {
	if _, err := cypherParams("user_id"); err == nil {
		t.Error("expected an error for an odd number of arguments")
	}
	if _, err := cypherParams(42, "value"); err == nil {
		t.Error("expected an error for a non-string key")
	}
}

// Parameters are JSON-encoded rather than interpolated into the Cypher text,
// so a value containing quotes or Cypher syntax cannot escape its string.
func TestCypherParams_EscapesHostileValues(t *testing.T) {
	hostile := `u1"}) DETACH DELETE (n) //`

	got, err := cypherParams("user_id", hostile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded map[string]string
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if decoded["user_id"] != hostile {
		t.Errorf("value = %q, want it preserved verbatim as %q", decoded["user_id"], hostile)
	}
	// The embedded quote must be escaped, so it cannot close the JSON string
	// and let the rest of the value be read as Cypher.
	if !strings.Contains(got, `\"`) {
		t.Errorf("embedded quote was not escaped in %s", got)
	}
	if len(decoded) != 1 {
		t.Errorf("decoded %d keys from %s, want exactly 1", len(decoded), got)
	}
}

func TestCypherParams_EmptyIsValidJSON(t *testing.T) {
	got, err := cypherParams()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "{}" {
		t.Errorf("cypherParams() = %q, want %q", got, "{}")
	}
}
