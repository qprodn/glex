package v1

import "google.golang.org/protobuf/proto"

// Redact implements the request redaction seam used by Kratos logging.Server.
func (x *CreateUserRequest) Redact() string {
	return redactCreateUserRequest(x)
}

// Redact implements the request redaction seam used by Kratos logging.Server.
func (x *UpdateUserRequest) Redact() string {
	if x == nil {
		return "<nil>"
	}
	redacted := proto.Clone(x).(*UpdateUserRequest)
	redactUserPassword(redacted.User)
	return redacted.String()
}

func redactCreateUserRequest(x *CreateUserRequest) string {
	if x == nil {
		return "<nil>"
	}
	redacted := proto.Clone(x).(*CreateUserRequest)
	redactUserPassword(redacted.User)
	return redacted.String()
}

func redactUserPassword(user *User) {
	if user != nil && user.Password != "" {
		user.Password = "***"
	}
}
