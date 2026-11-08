package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"io"
	"sort"
	"strings"

	wechat "glex/internal/domain/wechat"
)

// wxCrypto is WeChat's encrypted-mode messaging (安全模式). Third-party
// platforms accept only that mode. Plaintext and compatible mode are
// rejected before any platform state is touched.
type wxCrypto struct{}

// NewPushAuthenticator returns the production push authenticator.
func NewPushAuthenticator() PushAuthenticator { return wxCrypto{} }

const aesKeyLen = 43

func (wxCrypto) Authenticate(_ context.Context, req AuthenticateRequest) (*AuthenticatedPush, error) {
	tags, err := topLevelXML(req.Body)
	if err != nil {
		return nil, wechat.ErrPushRejected
	}
	encrypt := strings.TrimSpace(tags["Encrypt"])
	if !encryptedMode(req.EncryptType, tags) || encrypt == "" {
		return nil, wechat.ErrPushRejected
	}
	want := msgSignature(req.Token, req.Timestamp, req.Nonce, encrypt)
	got := strings.ToLower(strings.TrimSpace(req.MsgSignature))
	if len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return nil, wechat.ErrPushRejected
	}
	plain, err := (wxCrypto{}).decrypt(req.EncodingAESKey, encrypt, req.ComponentAppID)
	if err != nil {
		return nil, wechat.ErrPushRejected
	}
	return &AuthenticatedPush{XML: plain}, nil
}

func (c wxCrypto) SealReply(_ context.Context, req SealReplyRequest) (string, error) {
	enc, err := c.encrypt(req.EncodingAESKey, req.ComponentAppID, []byte(req.Plaintext))
	if err != nil {
		return "", wechat.ErrPushRejected
	}
	timestamp := replyTimestamp()
	nonce := req.Nonce
	sig := msgSignature(req.Token, timestamp, nonce, enc)
	var buf bytes.Buffer
	buf.WriteString("<xml>\n")
	buf.WriteString("<Encrypt><![CDATA[" + enc + "]]></Encrypt>\n")
	buf.WriteString("<MsgSignature><![CDATA[" + sig + "]]></MsgSignature>\n")
	buf.WriteString("<TimeStamp>" + timestamp + "</TimeStamp>\n")
	buf.WriteString("<Nonce><![CDATA[" + xmlCDATA(nonce) + "]]></Nonce>\n")
	buf.WriteString("</xml>")
	return buf.String(), nil
}

// encryptedMode reports WeChat's safe mode: encrypt_type=aes, an Encrypt
// field, and no plaintext message fields beside the wrapper. A body that
// also carries the plaintext fields is compatible mode. A body without
// Encrypt is plaintext. Both are rejected.
func encryptedMode(encryptType string, tags map[string]string) bool {
	if _, ok := tags["Encrypt"]; !ok {
		return false
	}
	for name := range tags {
		switch name {
		case "Encrypt", "ToUserName", "AppId":
		default:
			return false
		}
	}
	return strings.EqualFold(strings.TrimSpace(encryptType), "aes")
}

func topLevelXML(body []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	tags := map[string]string{}
	var current string
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return tags, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 {
				current = t.Name.Local
				tags[current] = ""
			}
		case xml.CharData:
			if depth == 2 && current != "" {
				tags[current] += string(t)
			}
		case xml.EndElement:
			if depth == 2 {
				current = ""
			}
			if depth > 0 {
				depth--
			}
		}
	}
}

func msgSignature(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func (wxCrypto) encrypt(encodingAESKey, appID string, msg []byte) (string, error) {
	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		return "", err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	buf := make([]byte, 0, 20+len(msg)+len(appID))
	buf = append(buf, random...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(msg)))
	buf = append(buf, msg...)
	buf = append(buf, appID...)
	buf = pkcs7Pad(buf, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(buf, buf)
	return base64.StdEncoding.EncodeToString(buf), nil
}

func (wxCrypto) decrypt(encodingAESKey, encryptB64, expectAppID string) ([]byte, error) {
	key, err := decodeAESKey(encodingAESKey)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encryptB64))
	if err != nil || len(raw) < aes.BlockSize || len(raw)%aes.BlockSize != 0 {
		return nil, wechat.ErrPushRejected
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(raw, raw)
	raw, err = pkcs7Unpad(raw, 32)
	if err != nil || len(raw) < 20 {
		return nil, wechat.ErrPushRejected
	}
	msgLen := int(binary.BigEndian.Uint32(raw[16:20]))
	if msgLen < 0 || 20+msgLen > len(raw) {
		return nil, wechat.ErrPushRejected
	}
	msg := raw[20 : 20+msgLen]
	if string(raw[20+msgLen:]) != expectAppID {
		return nil, wechat.ErrPushRejected
	}
	return msg, nil
}

func decodeAESKey(encodingAESKey string) ([]byte, error) {
	if len(encodingAESKey) != aesKeyLen {
		return nil, wechat.ErrPushRejected
	}
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil || len(key) != 32 {
		return nil, wechat.ErrPushRejected
	}
	return key, nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	n := blockSize - len(data)%blockSize
	if n == 0 {
		n = blockSize
	}
	return append(data, bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, wechat.ErrPushRejected
	}
	n := int(data[len(data)-1])
	if n == 0 || n > blockSize || n > len(data) {
		return nil, wechat.ErrPushRejected
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, wechat.ErrPushRejected
		}
	}
	return data[:len(data)-n], nil
}

func xmlCDATA(v string) string {
	return strings.ReplaceAll(v, "]]>", "]]]]><![CDATA[>")
}
