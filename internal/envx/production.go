package envx

import (
	"os"
	"strings"
)

// IsProduction is true when ENVIRONMENT is production or prod.
func IsProduction() bool {
	e := strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT")))
	return e == "production" || e == "prod"
}

// RequireAMQPS is true when REQUIRE_AMQPS=true or in production.
func RequireAMQPS() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("REQUIRE_AMQPS")))
	if v == "true" || v == "1" || v == "yes" {
		return true
	}
	return IsProduction()
}

// RequireMFAForAdmin is true when REQUIRE_MFA_FOR_ADMIN is set; admins must enroll MFA before login.
func RequireMFAForAdmin() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("REQUIRE_MFA_FOR_ADMIN")))
	return v == "true" || v == "1" || v == "yes"
}

// RequireMFAStepUpForAdmin requires a recent POST /v1/mfa/step-up token (X-MFA-Step-Up)
// on admin-only routes when the admin has MFA enabled.
func RequireMFAStepUpForAdmin() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("REQUIRE_MFA_STEP_UP_FOR_ADMIN")))
	return v == "true" || v == "1" || v == "yes"
}
