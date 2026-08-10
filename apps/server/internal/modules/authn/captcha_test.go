package authn

import "testing"

func TestIssueVerifyCaptcha(t *testing.T) {
	id, b64, answer, err := imageCaptcha.Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if id == "" || b64 == "" || answer == "" {
		t.Fatalf("empty captcha fields id=%q image=%v answer=%v", id, b64 != "", answer != "")
	}
	if !imageCaptcha.Verify(id, answer, true) {
		t.Fatal("correct answer must verify")
	}
	if imageCaptcha.Verify(id, answer, true) {
		t.Fatal("captcha must be one-shot: second verify must fail")
	}
}

func TestVerifyCaptchaRejectsWrongOrEmpty(t *testing.T) {
	id, _, answer, err := imageCaptcha.Generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wrong := "0000"
	if wrong == answer {
		wrong = "9999"
	}
	if wrong == answer {
		t.Skip("unlucky random match")
	}
	if VerifyCaptcha(id, wrong) {
		t.Fatal("wrong answer must not verify")
	}
	if VerifyCaptcha("", "1234") || VerifyCaptcha(id, "") {
		t.Fatal("empty id/code must not verify")
	}
}
