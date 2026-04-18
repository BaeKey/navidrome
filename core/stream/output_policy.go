package stream

import (
	"strings"

	"github.com/navidrome/navidrome/conf"
)

func AudioOutputEnabled() bool {
	return strings.TrimSpace(conf.Server.AudioOutputFormat) != ""
}

func AudioOutputFormat() string {
	return strings.ToLower(strings.TrimSpace(conf.Server.AudioOutputFormat))
}

func AudioOutputBitRate() int {
	return conf.Server.AudioMaxBitRate
}

func ApplyAudioOutput(format string, bitRate int) (string, int) {
	if !AudioOutputEnabled() {
		return format, bitRate
	}

	return AudioOutputFormat(), AudioOutputBitRate()
}

func ApplyAudioOutputToRequest(req Request) Request {
	if !AudioOutputEnabled() {
		return req
	}

	req.Format = AudioOutputFormat()
	req.BitRate = AudioOutputBitRate()
	req.SampleRate = 0
	req.BitDepth = 0
	req.Channels = 0
	req.Offset = 0
	return req
}

func ApplyAudioOutputToClientInfo(original *ClientInfo) *ClientInfo {
	if !AudioOutputEnabled() {
		return original
	}

	format := AudioOutputFormat()
	bitRate := AudioOutputBitRate()
	return &ClientInfo{
		Name:                       original.Name,
		Platform:                   original.Platform,
		MaxAudioBitrate:            bitRate,
		MaxTranscodingAudioBitrate: bitRate,
		DirectPlayProfiles: []DirectPlayProfile{
			{Containers: []string{format}, AudioCodecs: []string{format}, Protocols: []string{ProtocolHTTP}},
		},
		TranscodingProfiles: []Profile{
			{Container: format, AudioCodec: format, Protocol: ProtocolHTTP},
		},
	}
}
