package notifxses

import "github.com/Abraxas-365/manifesto/pkg/errx"

var sesErrors = errx.NewRegistry("NOTIFX_SES")

var (
	ErrSendFailed    = sesErrors.Register("SEND_FAILED", errx.TypeExternal, 500, "SES send email failed")
	ErrBulkFailed    = sesErrors.Register("BULK_FAILED", errx.TypeExternal, 500, "SES bulk send failed")
	ErrBuildMessage  = sesErrors.Register("BUILD_MESSAGE", errx.TypeInternal, 500, "Failed to build SES message")
)
