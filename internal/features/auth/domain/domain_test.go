package domain

import (
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	if got, err := NormalizeEmail("  Foo@Example.COM "); err != nil || got != "foo@example.com" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "nope", "a b@c.com", "Name <a@b.com>"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	for _, bad := range []string{"short1", "onlyletters", "12345678"} {
		if ValidatePassword(bad) == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	if err := ValidatePassword("goodpass1"); err != nil {
		t.Fatal(err)
	}
}

func TestOTPLifecycle(t *testing.T) {
	now := time.Now()
	o := NewOTP("1", "u1", VerifyEmail, "123456", now)
	if !o.Matches("123456") || o.Matches("654321") {
		t.Fatal("code matching is wrong")
	}
	if !o.Usable(now) || o.Usable(now.Add(OTPTTL+time.Second)) {
		t.Fatal("expiry is wrong")
	}
	if !o.CoolingDown(now.Add(OTPCooldown-time.Second)) || o.CoolingDown(now.Add(OTPCooldown)) {
		t.Fatal("cooldown is wrong")
	}
	code, err := GenerateCode()
	if err != nil || len(code) != 6 {
		t.Fatalf("code %q, %v", code, err)
	}
}
