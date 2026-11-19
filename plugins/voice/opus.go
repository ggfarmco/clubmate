package main

import (
	"errors"
	"fmt"

	"github.com/pion/opus"
)

const (
	formatPCM  = "pcm"
	formatOpus = "opus"

	sampleRate       = 16000
	maxOpusPacket    = 1275
	maxCallPackets   = 512
	maxPacketSamples = sampleRate * 120 / 1000
)

var (
	errWrongFormat    = errors.New("audio field does not match the utterance format")
	errEmptyPacket    = errors.New("empty opus packet")
	errPacketTooLarge = fmt.Errorf("opus packet over %d bytes", maxOpusPacket)
	errTooManyPackets = fmt.Errorf("over %d opus packets in one call", maxCallPackets)
)

type opusDecoder struct {
	decoder opus.Decoder
	samples []int16
}

func newOpusDecoder() (*opusDecoder, error) {
	d, err := opus.NewDecoderWithOutput(sampleRate, 1)
	if err != nil {
		return nil, fmt.Errorf("opus decoder: %w", err)
	}
	return &opusDecoder{decoder: d, samples: make([]int16, maxPacketSamples)}, nil
}

func checkPackets(packets [][]byte) error {
	if len(packets) > maxCallPackets {
		return errTooManyPackets
	}
	total := 0
	for _, p := range packets {
		switch {
		case len(p) == 0:
			return errEmptyPacket
		case len(p) > maxOpusPacket:
			return errPacketTooLarge
		}
		total += len(p)
	}
	if total > maxChunkBytes {
		return errChunkTooLarge
	}
	return nil
}

func (o *opusDecoder) decode(packets [][]byte, limit int) ([]byte, error) {
	pcm := make([]byte, 0, min(limit, len(packets)*2*sampleRate/50))
	for i, p := range packets {
		if len(pcm) >= limit {
			break
		}
		n, err := o.decoder.DecodeToInt16(p, o.samples)
		if err != nil {
			return nil, fmt.Errorf("opus packet %d: %w", i, err)
		}
		for _, s := range o.samples[:n] {
			pcm = append(pcm, byte(s), byte(uint16(s)>>8))
		}
	}
	return pcm[:min(len(pcm), limit)], nil
}
