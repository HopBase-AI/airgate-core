package dto

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestCreateRelayDetectionReqAcceptsAutoPlatform(t *testing.T) {
	validate := validator.New()
	validate.SetTagName("binding")
	req := CreateRelayDetectionReq{
		BaseURL:      "https://relay.example.com",
		APIKey:       "sk-test",
		PlatformType: "auto",
	}
	if err := validate.Struct(req); err != nil {
		t.Fatalf("auto platform should pass request validation: %v", err)
	}

	req.PlatformType = "unsupported"
	if err := validate.Struct(req); err == nil {
		t.Fatal("unsupported platform should fail request validation")
	}
}
