package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	_ "image/png"
	"os"
	"strings"
	"sync"
)

const screenModel = "fixture-screen-model"
const screenQuestion = "Describe the synthetic fixture after redaction."
const screenFollowup = "Summarize that observation without sending another image."
const screenSummary = `{"summary":"Synthetic fixture inspection","detectedText":["synthetic"],"applications":["Fixture"],"windows":["Synthetic display"],"uiElements":[{"type":"region","label":"Red region","bounds":{"x":600,"y":500,"width":300,"height":400},"confidence":0.95,"description":"<b>literal fixture markup</b>"}],"issues":[],"suggestions":[],"uncertainties":[]}`

type screenRequestAudit struct {
	Kind       string `json:"kind"`
	ImageParts int    `json:"imageParts"`
	Bytes      int    `json:"bytes,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type screenAudit struct {
	mu   sync.Mutex
	path string
}

// inspect validates the actual Chat Completions body received on loopback.
// It appends only request type, counts, dimensions and a digest to the audit.
func (a *screenAudit) inspect(encoded []byte) (string, error) {
	type part struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL    string `json:"url"`
			Detail string `json:"detail"`
		} `json:"image_url"`
	}
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &request); err != nil || len(request.Messages) == 0 {
		return "", errors.New("missing chat messages")
	}

	var row screenRequestAudit
	question := ""
	for i, message := range request.Messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			if message.Role == "user" && i == len(request.Messages)-1 {
				question = text
			}
			continue
		}
		var parts []part
		if err := json.Unmarshal(message.Content, &parts); err != nil {
			return "", errors.New("invalid message content")
		}
		for _, item := range parts {
			switch item.Type {
			case "text":
				if message.Role == "user" && i == len(request.Messages)-1 {
					question = item.Text
				}
			case "image_url":
				row.ImageParts++
				if row.ImageParts != 1 || i != len(request.Messages)-1 || message.Role != "user" {
					return "", errors.New("unexpected image part")
				}
				value, ok := strings.CutPrefix(item.ImageURL.URL, "data:image/png;base64,")
				if !ok {
					return "", errors.New("expected PNG fixture")
				}
				pixels, err := base64.StdEncoding.DecodeString(value)
				if err != nil {
					return "", errors.New("invalid PNG encoding")
				}
				img, kind, err := image.Decode(bytes.NewReader(pixels))
				if err != nil || kind != "png" || img.Bounds().Dx() != 80 || img.Bounds().Dy() != 60 ||
					color.RGBAModel.Convert(img.At(8, 8)) != (color.RGBA{A: 255}) ||
					color.RGBAModel.Convert(img.At(58, 42)) != (color.RGBA{R: 221, G: 61, B: 75, A: 255}) {
					return "", errors.New("unexpected fixture pixels")
				}
				sum := sha256.Sum256(pixels)
				row.Bytes = len(pixels)
				row.SHA256 = hex.EncodeToString(sum[:])
				row.Width, row.Height = img.Bounds().Dx(), img.Bounds().Dy()
				row.Detail = item.ImageURL.Detail
			default:
				return "", errors.New("unexpected message part")
			}
		}
	}
	var answer string
	switch {
	case row.ImageParts == 1 && question == screenQuestion && row.Detail == "auto":
		row.Kind = "screen"
		answer = screenSummary
	case row.ImageParts == 0 && question == screenFollowup && !bytes.Contains(encoded, []byte("data:image/")):
		row.Kind = "followup"
		answer = "Fixture text-only follow-up."
	default:
		return "", errors.New("unexpected screen fixture request")
	}
	return answer, a.write(row)
}

func (a *screenAudit) write(row screenRequestAudit) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	file, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(row)
}
