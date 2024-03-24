package tokenrefresher

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

const JwtFmt = ".%s."

func Test_isTokenValid(t *testing.T) {
	type args struct {
		token string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"Reject empty token",
			args{""},
			false,
		},
		{
			"Reject invalid token",
			args{"thisisnotatoken"},
			false,
		},
		{
			"Reject ill-formated token",
			args{fmt.Sprintf(JwtFmt, "notb64string")},
			false,
		},
		{
			"Reject token about to expire",
			args{getTokenWithExpiry(time.Hour)},
			false,
		},
		{
			"Reject token that has expired",
			args{getTokenWithExpiry(-time.Second)},
			false,
		},
		{
			"Accept token with enough expiry",
			args{getTokenWithExpiry(time.Hour * 2)},
			true,
		},
		{
			"Accept token with enough expiry",
			args{getTokenWithExpiry(time.Hour * 48)},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTokenValid(tt.args.token, time.Minute*90); got != tt.want {
				t.Errorf("isTokenValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func getTokenWithExpiry(exp time.Duration) string {
	expiresAt := time.Now().Add(exp)
	data := fmt.Sprintf(`{"exp":%v}`, expiresAt.Unix())
	claims := base64.RawURLEncoding.EncodeToString([]byte(data))
	return fmt.Sprintf(JwtFmt, claims)
}
