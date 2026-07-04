package web

import "testing"

func TestParseTargets(t *testing.T) {
	tests := []struct {
		input        string
		want         []string
		wantRejected int
		wantErr      bool
	}{
		{"192.168.1.1", []string{"192.168.1.1"}, 0, false},
		{"a.example.com, b.example.com", []string{"a.example.com", "b.example.com"}, 0, false},
		{"a.example.com\nb.example.com\n", []string{"a.example.com", "b.example.com"}, 0, false},
		{"a.example.com a.example.com b.example.com", []string{"a.example.com", "b.example.com"}, 0, false}, // dedup
		{"a.example.com, 10.0.0.0/8", []string{"a.example.com"}, 1, false},                                  // tolerant: skip the bad one
		{"10.0.0.0/8, ftp://x.example.com", nil, 2, true},                                                   // all invalid -> error, both reported
		{"  \n , ", nil, 0, true}, // only separators
		{"", nil, 0, true},
	}

	for _, tt := range tests {
		got, rejected, err := ParseTargets(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseTargets(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if len(rejected) != tt.wantRejected {
			t.Errorf("ParseTargets(%q) rejected = %d, want %d", tt.input, len(rejected), tt.wantRejected)
		}
		if !tt.wantErr {
			if len(got) != len(tt.want) {
				t.Errorf("ParseTargets(%q) = %v, want %v", tt.input, got, tt.want)
				continue
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseTargets(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		}
	}
}

func TestValidateMode(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"quick", "quick", false},
		{"full", "full", false},
		{"", "quick", false},
		{"QUICK", "quick", false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		got, err := ValidateMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if got != tt.want && !tt.wantErr {
			t.Errorf("ValidateMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
