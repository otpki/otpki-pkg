package mocks

import (
	"time"

	"github.com/otpki/otpki-pkg/helpers"
)

func MockAdvanceTime(d time.Duration) {
	helpers.MockOffset = helpers.MockOffset + d
}

func MockResetTime() {
	helpers.MockOffset = time.Duration(0)
}
