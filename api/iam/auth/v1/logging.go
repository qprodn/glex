package v1

import "google.golang.org/protobuf/proto"

// Redact implements the request redaction seam used by Kratos logging.Server.
func (x *LoginRequest) Redact() string {
	if x == nil {
		return "<nil>"
	}
	redacted := proto.Clone(x).(*LoginRequest)
	if redacted.Password != "" {
		redacted.Password = "***"
	}
	return redacted.String()
}
