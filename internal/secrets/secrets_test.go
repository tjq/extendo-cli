package secrets

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	const (
		jwt = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			"eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ." +
			"SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
		githubToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	)

	tests := []struct {
		name     string
		input    string
		expected Category
		wantOK   bool
	}{
		{
			name:     "credit card grouped by spaces",
			input:    "4111 1111 1111 1111",
			expected: CategoryCreditCard,
			wantOK:   true,
		},
		{
			name:     "credit card inside a sentence",
			input:    "card 4111-1111-1111-1111 expires soon",
			expected: CategoryCreditCard,
			wantOK:   true,
		},
		{
			name:     "ssn",
			input:    "123-45-6789",
			expected: CategorySSN,
			wantOK:   true,
		},
		{
			name:     "jwt",
			input:    jwt,
			expected: CategoryJWT,
			wantOK:   true,
		},
		{
			name:     "aws access key",
			input:    "AKIAIOSFODNN7EXAMPLE",
			expected: CategoryAWSAccessKey,
			wantOK:   true,
		},
		{
			name:     "aws session key",
			input:    "export AWS_ACCESS_KEY_ID=ASIAIOSFODNN7EXAMPLE",
			expected: CategoryAWSAccessKey,
			wantOK:   true,
		},
		{
			name:     "github personal access token",
			input:    githubToken,
			expected: CategoryGitHubToken,
			wantOK:   true,
		},
		{
			name:     "github fine-grained token",
			input:    "github_pat_" + strings.Repeat("a", 82),
			expected: CategoryGitHubToken,
			wantOK:   true,
		},
		{
			name:     "anthropic key",
			input:    "sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
			expected: CategoryAnthropicKey,
			wantOK:   true,
		},
		{
			name:     "openai project key",
			input:    "sk-proj-AbCdEfGhIjKlMnOpQrStUv0123456789",
			expected: CategoryOpenAIKey,
			wantOK:   true,
		},
		{
			// Concatenated rather than written out. The value is invented, but it
			// is shaped exactly like a real Stripe key — which is the point of
			// the case, and also what makes GitHub's push protection reject the
			// file when the literal appears whole in the source.
			name:     "stripe secret key",
			input:    "sk_live_" + strings.Repeat("AbCdEfGh", 3),
			expected: CategoryStripeKey,
			wantOK:   true,
		},
		{
			name:     "slack token",
			input:    "xoxb-abcdefghijklmnopqrstuvwxyz012345",
			expected: CategorySlackToken,
			wantOK:   true,
		},
		{
			name:     "openssh private key header",
			input:    "-----BEGIN OPENSSH PRIVATE KEY-----",
			expected: CategoryPrivateKey,
			wantOK:   true,
		},
		{
			name:     "pgp private key block header",
			input:    "-----BEGIN PGP PRIVATE KEY BLOCK-----\nlgAEBFy...",
			expected: CategoryPrivateKey,
			wantOK:   true,
		},
		{
			name:     "totp on its own",
			input:    "123456",
			expected: CategoryTOTP,
			wantOK:   true,
		},
		{
			name:     "eight digit totp surrounded by whitespace",
			input:    "  12345678\n",
			expected: CategoryTOTP,
			wantOK:   true,
		},
		{
			name:     "generic api key assignment",
			input:    `api_key = "s3cr3tvalue0123456789abcdef"`,
			expected: CategoryGenericAPIKey,
			wantOK:   true,
		},
		{
			name:     "specific pattern wins over generic api key",
			input:    "token: " + githubToken,
			expected: CategoryGitHubToken,
			wantOK:   true,
		},
		{
			name:     "anthropic key wins over openai key",
			input:    "sk-ant-api03-0123456789abcdefghijklmnop",
			expected: CategoryAnthropicKey,
			wantOK:   true,
		},
		{name: "plain prose", input: "hello world"},
		{name: "empty", input: ""},
		{name: "whitespace only", input: "   \n\t "},
		{name: "git sha", input: "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3"},
		{name: "uuid", input: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "digits with trailing words are not totp", input: "123456 apples"},
		{name: "digits on a line of their own are not totp", input: "123456\napples"},
		{name: "five digits are too short for totp", input: "12345"},
		{name: "nine digits are too long for totp", input: "123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Classify(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("Classify(%q) ok = %v, expected %v (category %q)", tt.input, ok, tt.wantOK, got)
			}

			if got != tt.expected {
				t.Fatalf("Classify(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestClassifySkipsHugeInput(t *testing.T) {
	huge := strings.Repeat("a", 64*1024) + " AKIAIOSFODNN7EXAMPLE"

	if got, ok := Classify(huge); ok {
		t.Fatalf("Classify(huge) = %q, true; expected no match", got)
	}
}

func TestCategoryLabel(t *testing.T) {
	tests := []struct {
		category Category
		expected string
	}{
		{CategoryCreditCard, "Credit card"},
		{CategorySSN, "SSN"},
		{CategoryJWT, "JWT"},
		{CategoryAWSAccessKey, "AWS key"},
		{CategoryGitHubToken, "GitHub token"},
		{CategoryPrivateKey, "Private key"},
		{CategoryTOTP, "TOTP code"},
		{CategoryAnthropicKey, "Anthropic key"},
		{CategoryOpenAIKey, "OpenAI key"},
		{CategoryStripeKey, "Stripe key"},
		{CategorySlackToken, "Slack token"},
		{CategoryGenericAPIKey, "API key"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			if got := tt.category.Label(); got != tt.expected {
				t.Fatalf("Category(%q).Label() = %q, expected %q", tt.category, got, tt.expected)
			}
		})
	}
}

// TestCategoryRawValues pins the raw values to the Swift enum they mirror.
func TestCategoryRawValues(t *testing.T) {
	expected := []Category{
		"creditCard",
		"ssn",
		"jwt",
		"awsAccessKey",
		"githubToken",
		"privateKey",
		"totp",
		"anthropicKey",
		"openaiKey",
		"stripeKey",
		"slackToken",
		"genericApiKey",
	}

	actual := []Category{
		CategoryCreditCard,
		CategorySSN,
		CategoryJWT,
		CategoryAWSAccessKey,
		CategoryGitHubToken,
		CategoryPrivateKey,
		CategoryTOTP,
		CategoryAnthropicKey,
		CategoryOpenAIKey,
		CategoryStripeKey,
		CategorySlackToken,
		CategoryGenericAPIKey,
	}

	for i, want := range expected {
		if actual[i] != want {
			t.Errorf("category %d = %q, expected %q", i, actual[i], want)
		}
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: "••••••••"},
		{name: "eight characters", input: "12345678", expected: "••••••••"},
		{name: "nine characters", input: "123456789", expected: "1234567••••••••••"},
		{
			name:     "long single line keeps seven runes",
			input:    "sk-ant-api03-abcdefghijklmnop",
			expected: "sk-ant-••••••••••",
		},
		{
			name:     "multiline uses the first line only",
			input:    "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk=",
			expected: "-----BE••••••••••",
		},
		{
			name:     "short first line is kept whole",
			input:    "ab\ncdefghijklmno",
			expected: "ab••••••••••",
		},
		{
			name:     "counts runes not bytes",
			input:    "héllo wörld über",
			expected: "héllo w••••••••••",
		},
		{
			name:     "carriage return ends the first line",
			input:    "abcdefghijkl\r\nsecond",
			expected: "abcdefg••••••••••",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Mask(tt.input); got != tt.expected {
				t.Fatalf("Mask(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}
