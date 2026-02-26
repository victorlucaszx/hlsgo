package main

import (
	"context"
	"sync"
)

type QualitySettings struct {
	Scale   string
	Bitrate string
	MaxRate string
	BufSize string
}

var QualityMap = map[string]QualitySettings{
	"240p":  {Scale: "-2:240", Bitrate: "400k", MaxRate: "428k", BufSize: "600k"},
	"360p":  {Scale: "-2:360", Bitrate: "800k", MaxRate: "856k", BufSize: "1200k"},
	"480p":  {Scale: "-2:480", Bitrate: "1400k", MaxRate: "1498k", BufSize: "2100k"},
	"720p":  {Scale: "-2:720", Bitrate: "2800k", MaxRate: "2996k", BufSize: "4200k"},
	"1080p": {Scale: "-2:1080", Bitrate: "5000k", MaxRate: "5350k", BufSize: "7500k"},
	"1440p": {Scale: "-2:1440", Bitrate: "8000k", MaxRate: "8560k", BufSize: "12000k"},
	"2160p": {Scale: "-2:2160", Bitrate: "14000k", MaxRate: "14980k", BufSize: "21000k"},
}

var QualityBandwidth = map[string]int{
	"240p":  400000,
	"360p":  800000,
	"480p":  1400000,
	"720p":  2800000,
	"1080p": 5000000,
	"1440p": 8000000,
	"2160p": 14000000,
}

var QualityResolution = map[string]string{
	"240p":  "426x240",
	"360p":  "640x360",
	"480p":  "854x480",
	"720p":  "1280x720",
	"1080p": "1920x1080",
	"1440p": "2560x1440",
	"2160p": "3840x2160",
}

type ConvertRequest struct {
	MediaFileID  int      `json:"media_file_id"`
	Title        string   `json:"title"`
	S3Path       string   `json:"s3_path"`
	Width        int      `json:"width"`
	Height       int      `json:"height"`
	Duration     int      `json:"duration"`
	Qualities    []string `json:"qualities"`
	CallbackURL  string   `json:"callback_url"`
	CloudfrontURL string  `json:"cloudfront_url"`
}

type ConvertResponse struct {
	ConversionID string `json:"conversion_id"`
	Message      string `json:"message"`
}

type CancelResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type CallbackPayload struct {
	MediaID      int    `json:"media_id"`
	Quality      string `json:"quality"`
	Status       string `json:"status"`
	S3Path       string `json:"s3_path,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type ConversionJob struct {
	ID                string
	Request           ConvertRequest
	Cancel            context.CancelFunc
	Ctx               context.Context
	CompletedQualities []string
	Mu                sync.Mutex
}
