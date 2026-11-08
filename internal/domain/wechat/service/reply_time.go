package service

import (
	"strconv"
	"time"
)

func replyTimestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}
