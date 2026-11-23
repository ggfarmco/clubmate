package main

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noRelay = "http://127.0.0.1:1"

func TestNotNowKeepsTheCompanionDismissedAcrossARestart(t *testing.T) {
	e := newRemoteEnv(t, noRelay, map[string]any{pushRelayModeKey: relayOff})

	fresh := e.status()
	refused := e.c.Call(t.Context(), "command.run", map[string]any{"id": "remote.dismiss-offer", "workspaceId": "", "caller": "agt_x"}, nil)
	var dismissed status
	require.NoError(t, e.run("remote.dismiss-offer", nil, &dismissed))
	e.restart()

	assert.Equal(t, companionView{State: companionUnpaired}, fresh.Companion)
	assert.Error(t, refused)
	assert.Equal(t, companionDismissed, dismissed.Companion.State)
	assert.Equal(t, companionDismissed, e.status().Companion.State)
	assert.Equal(t, strconv.Quote(companionDismissed), e.stored(companionKey))
}

func TestAPairingEndsTheOfferForGoodEvenAfterTheLastDeviceIsRevoked(t *testing.T) {
	e := newRemoteEnv(t, noRelay, map[string]any{pushRelayModeKey: relayOff})

	var pr pairResult
	require.NoError(t, e.run("remote.pair", nil, &pr))
	link, err := url.Parse(pr.Link)
	require.NoError(t, err)
	host := link.Query().Get("h")
	res := post(t, "http://"+net.JoinHostPort(host, link.Query().Get("p")), "/v1/pair", "", pairRequest{Code: pr.Code, Device: deviceInfo{ID: "phone", Name: "iPhone", Model: "iPhone"}})
	res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	paired := e.status()
	require.NoError(t, e.run("remote.revoke", map[string]any{"device": "phone"}, nil))

	assert.Equal(t, companionView{State: companionPaired, Via: viaTailscale, Host: host}, paired.Companion)
	assert.Equal(t, companionDismissed, e.status().Companion.State)
	assert.Equal(t, strconv.Quote(companionDismissed), e.stored(companionKey))
}

func TestAnInstallThatPairedBeforeTheStateExistedStaysDismissedAfterARestart(t *testing.T) {
	old := device{ID: "phone", Name: "iPhone", Model: "iPhone", TokenHash: hashToken("cmt_old")}
	e := newRemoteEnv(t, noRelay, map[string]any{pushRelayModeKey: relayOff, deviceKey(old.ID): old})

	paired := e.status()
	require.NoError(t, e.run("remote.revoke", map[string]any{"device": old.ID}, nil))
	e.restart()

	assert.Equal(t, companionPaired, paired.Companion.State)
	assert.Equal(t, companionDismissed, e.status().Companion.State)
	assert.Equal(t, strconv.Quote(companionDismissed), e.stored(companionKey))
}

func TestTheOfferStaysAcrossARestartAndNeverReopensAfterNotNow(t *testing.T) {
	e := newRemoteEnv(t, noRelay, map[string]any{pushRelayModeKey: relayOff})

	refused := e.c.Call(t.Context(), "command.run", map[string]any{"id": "remote.offer", "workspaceId": "", "caller": "agt_x"}, nil)
	var offered status
	require.NoError(t, e.run("remote.offer", nil, &offered))
	e.restart()
	kept := e.status()
	require.NoError(t, e.run("remote.dismiss-offer", nil, nil))
	var again status
	require.NoError(t, e.run("remote.offer", nil, &again))

	assert.Error(t, refused)
	assert.Equal(t, companionOffer, offered.Companion.State)
	assert.Equal(t, companionOffer, kept.Companion.State)
	assert.Equal(t, companionDismissed, again.Companion.State)
}
