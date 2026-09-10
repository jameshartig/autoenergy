package server

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/controller"
	"github.com/raterudder/raterudder/pkg/ess"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/crypto/hkdf"
)

// Tunable notification thresholds for anomaly detection.
// These constants are grouped here for easy iteration and simulation against actual site history.
const (
	// defaultSummaryFlavor is the default notification flavor for morning and evening summaries.
	defaultSummaryFlavor = "home_planner"

	// Default duration to wait before verifying a grid outage isn't a temporary blip.
	defaultGridOutageDelay = 5 * time.Minute

	// Deduplication window for VPP dispatches (allows separate morning/evening events).
	vppDispatchDeduplicationWindow = 5 * time.Hour

	// Minimum battery SOC increase (%) required to mention a projected peak in the morning summary.
	morningSummaryMinPeakDeltaSOC = 5.0

	// Multiplier for price surge to be considered a significant change (at least 20% higher than previous alert).
	priceSpikeSignificantMultiplier = 1.20

	// Escalated percentile tiers required for re-alerting within 1-6 hours on significant change.
	// Within 1 to 6 hours of a previous alert, a new alert is only sent if the price is at least 20%
	// higher than the previous alerted peak price AND meets these higher percentile thresholds.
	priceSpikeEscalatedPercentileLow    = 0.98 // Top 2% of all hours
	priceSpikeEscalatedPercentileMedium = 0.95 // Top 5% of all hours
	priceSpikeEscalatedPercentileHigh   = 0.95 // Top 5% of all hours

	// Minimum absolute price ($/kWh) before any price spike alert can trigger.
	// Prevents alerts during extremely cheap periods (e.g., prices jumping from 2¢ to 4¢/kWh).
	priceSpikeAbsoluteFloorDollarsPerKWH = 0.18

	// Minimum delta ($/kWh) that upcoming price must exceed the reference price to be considered a spike.
	// Low sensitivity alerts only on large surges ($0.10/kWh above reference);
	// Medium alerts on moderate surges ($0.05/kWh); High alerts on smaller surges ($0.03/kWh).
	priceSpikeMinDeltaLow    = 0.10
	priceSpikeMinDeltaMedium = 0.05
	priceSpikeMinDeltaHigh   = 0.03

	// Percentile of historical prices required to qualify as a price spike:
	// - High sensitivity: Top 10% (0.90) of all-hours alone (no time-of-day restriction; alerts whenever price is high).
	// - Medium sensitivity: Top 10% (0.90) of all-hours AND time-of-day relative (+/- 1 hour buffer).
	// - Low sensitivity: Top 5% (0.95) of all-hours AND time-of-day relative (+/- 1 hour buffer).
	priceSpikePercentileLow    = 0.95
	priceSpikePercentileMedium = 0.90
	priceSpikePercentileHigh   = 0.90

	// Minimum weather forecast solar (kW) required before solar underproduction is evaluated.
	solarUnderproductionMinForecastKW = 3.0

	// Minimum absolute deficit (kW) between forecast and actual generation to trigger alert.
	solarUnderproductionMinDeficitKW = 2.5

	// Ratio of actual generation to forecasted generation below which an alert is triggered.
	solarUnderproductionRatioLow    = 0.50 // 50% of forecast (noticeable underproduction / dirty panels)
	solarUnderproductionRatioMedium = 0.30 // 30% of forecast (moderate underproduction / partial string failure)
	solarUnderproductionRatioHigh   = 0.15 // 15% of forecast (severe underproduction / inverter offline)

	// Maximum cloud cover percentage above which underproduction alerts are suppressed (clouds explain deficit).
	solarUnderproductionMaxCloudCoverPercent = 60.0
)

// decodeBase64Key decodes a base64 string using base64.RawURLEncoding (RFC 7515 / RFC 8291).
func decodeBase64Key(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(s), "="))
}

// parseVAPIDPrivateKey parses a base64-encoded 32-byte ECDSA P-256 private key scalar.
func parseVAPIDPrivateKey(privateKeyStr string) (*ecdsa.PrivateKey, string, error) {
	privBytes, err := decodeBase64Key(privateKeyStr)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode private key base64: %w", err)
	}

	if len(privBytes) != 32 {
		return nil, "", fmt.Errorf("invalid private key length: expected 32 bytes, got %d", len(privBytes))
	}

	ecdhKey, err := ecdh.P256().NewPrivateKey(privBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse private key scalar: %w", err)
	}

	pubBytes := ecdhKey.PublicKey().Bytes()
	publicKeyStr := base64.RawURLEncoding.EncodeToString(pubBytes)

	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(pubBytes[1:33]),
			Y:     new(big.Int).SetBytes(pubBytes[33:65]),
		},
		D: new(big.Int).SetBytes(privBytes),
	}

	return priv, publicKeyStr, nil
}

// generateMorningSummary generates copy for the morning summary report based on selected flavor.
func generateMorningSummary(
	flavor string,
	currentStatus types.SystemStatus,
	todayForecastKWH float64,
	yesterdayActualKWH float64,
	peakSolarKWH float64,
	hitCapacityAt time.Time,
	projectedPeakSOC float64,
	timeLoc *time.Location,
) (string, string) {
	if timeLoc == nil {
		timeLoc = time.UTC
	}
	if projectedPeakSOC <= 0 {
		projectedPeakSOC = currentStatus.BatterySOC
	}

	currentEnergyKWH := currentStatus.BatterySOC * currentStatus.BatteryCapacityKWH / 100.0

	deltaPct := 0.0
	hasYesterday := yesterdayActualKWH > 0.5
	if hasYesterday {
		deltaPct = ((todayForecastKWH - yesterdayActualKWH) / yesterdayActualKWH) * 100.0
	}

	var relWording string
	if !hasYesterday {
		relWording = fmt.Sprintf("%.1f kWh expected", todayForecastKWH)
	} else if deltaPct >= 10.0 {
		relWording = fmt.Sprintf("+%.0f%% vs yesterday", deltaPct)
	} else if deltaPct <= -10.0 {
		relWording = fmt.Sprintf("%.0f%% vs yesterday", deltaPct)
	} else {
		relWording = "similar to yesterday"
	}

	hitCapacityStr := ""
	if !hitCapacityAt.IsZero() {
		hitCapacityStr = hitCapacityAt.In(timeLoc).Format("3:04 PM")
	}

	solarRatio := 0.0
	if peakSolarKWH > 0 {
		solarRatio = todayForecastKWH / peakSolarKWH
	} else if todayForecastKWH > 0 {
		solarRatio = 1.0
	}

	switch flavor {
	case "home_planner":
		var title, body string
		if solarRatio >= 0.80 {
			if hitCapacityStr != "" {
				title = "☀️ Great Solar Day Ahead"
			} else {
				title = "☀️ Clear Skies Ahead"
			}
		} else if solarRatio >= 0.45 {
			title = "⛅ Moderate Solar Outlook"
		} else {
			title = "☁️ Low Solar Outlook"
		}

		if hitCapacityStr != "" {
			body = fmt.Sprintf("Battery at %.0f%%. Full battery expected by %s.", currentStatus.BatterySOC, hitCapacityStr)
		} else if solarRatio >= 0.80 {
			if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
				body = fmt.Sprintf("Battery at %.0f%% (peaking ~%.0f%%). Strong solar today (%s) will help cover daytime home usage.", currentStatus.BatterySOC, projectedPeakSOC, relWording)
			} else {
				body = fmt.Sprintf("Battery at %.0f%%. Strong solar today (%s) will help cover daytime home usage.", currentStatus.BatterySOC, relWording)
			}
		} else if solarRatio >= 0.45 {
			if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
				body = fmt.Sprintf("Battery at %.0f%% (peaking ~%.0f%%). Moderate solar expected today (%s).", currentStatus.BatterySOC, projectedPeakSOC, relWording)
			} else {
				body = fmt.Sprintf("Battery at %.0f%%. Moderate solar expected today (%s); solar will help cover baseline load.", currentStatus.BatterySOC, relWording)
			}
		} else {
			body = fmt.Sprintf("Battery at %.0f%%. Solar will be limited today (%s). Consider avoiding heavy loads.", currentStatus.BatterySOC, relWording)
		}
		return title, body

	case "executive":
		title := fmt.Sprintf("☀️ %.1f kWh Solar Expected • 🔋 %.0f%% SOC", todayForecastKWH, currentStatus.BatterySOC)
		var body string
		if hitCapacityStr != "" {
			if solarRatio >= 0.70 {
				body = fmt.Sprintf("Great solar today; battery will fully top off by %s.", hitCapacityStr)
			} else {
				body = fmt.Sprintf("Battery will reach full charge by %s (%s).", hitCapacityStr, relWording)
			}
		} else if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
			body = fmt.Sprintf("Solar expected: %.1f kWh (%s). Battery projected to reach ~%.0f%%.", todayForecastKWH, relWording, projectedPeakSOC)
		} else {
			body = fmt.Sprintf("Solar limited today (%s); battery will supply home without charging.", relWording)
		}
		return title, body

	case "pilot":
		title := "🤖 RateRudder: Morning Outlook"
		var body string
		if hitCapacityStr != "" {
			body = fmt.Sprintf("Battery at %.0f%%. Forecast shows %.1f kWh solar refilling battery by %s. Optimizing daytime self-consumption.", currentStatus.BatterySOC, todayForecastKWH, hitCapacityStr)
		} else if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
			body = fmt.Sprintf("Battery at %.0f%% (peaking ~%.0f%%). Forecast shows %.1f kWh solar today. Optimizing self-consumption to defend peak hours.", currentStatus.BatterySOC, projectedPeakSOC, todayForecastKWH)
		} else {
			body = fmt.Sprintf("Battery at %.0f%%. Solar limited (%.1f kWh). Preserving battery reserve to defend peak pricing hours.", currentStatus.BatterySOC, todayForecastKWH)
		}
		return title, body

	case "metrics_heavy":
		fallthrough
	default:
		title := fmt.Sprintf("🔋 %.0f%% SOC (%.1f kWh) • ☀️ %.1f kWh Solar", currentStatus.BatterySOC, currentEnergyKWH, todayForecastKWH)
		var body string
		if hitCapacityStr != "" {
			body = fmt.Sprintf("Forecast: %s. Full charge expected by %s.", relWording, hitCapacityStr)
		} else if projectedPeakSOC >= currentStatus.BatterySOC+morningSummaryMinPeakDeltaSOC {
			body = fmt.Sprintf("Forecast: %s. Battery projected to peak at ~%.0f%% today.", relWording, projectedPeakSOC)
		} else {
			body = fmt.Sprintf("Forecast: %s. Battery not projected to charge today (currently %.0f%%).", relWording, currentStatus.BatterySOC)
		}
		return title, body
	}
}

// generateEveningSummary generates copy for the evening summary report based on selected flavor.
func generateEveningSummary(
	flavor string,
	currentStatus types.SystemStatus,
	todayActualSolarKWH float64,
	todayHomeUsageKWH float64,
	todayGridExportKWH float64,
	todayGridImportKWH float64,
	minBatterySOC float64,
	hitDeficitAt time.Time,
	timeLoc *time.Location,
) (string, string) {
	if timeLoc == nil {
		timeLoc = time.UTC
	}
	currentEnergyKWH := currentStatus.BatterySOC * currentStatus.BatteryCapacityKWH / 100.0

	var deficitStr string
	if !hitDeficitAt.IsZero() {
		deficitStr = hitDeficitAt.In(timeLoc).Format("3:04 PM")
	}

	reserveThreshold := minBatterySOC
	if reserveThreshold <= 0 {
		reserveThreshold = 20.0
	}

	switch flavor {
	case "home_planner":
		title := "🌙 Evening Energy Wrap-up"
		var body string
		if hitDeficitAt.IsZero() {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Projected to power home through the night until tomorrow's solar.", currentStatus.BatterySOC, currentEnergyKWH)
		} else if currentStatus.BatterySOC <= reserveThreshold+2.0 || hitDeficitAt.Before(currentStatus.Timestamp.In(timeLoc).Add(30*time.Minute)) {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Reserve is low; home will switch to grid power shortly.", currentStatus.BatterySOC, currentEnergyKWH)
		} else {
			body = fmt.Sprintf("Battery at %.0f%% (%.1f kWh). Projected to supply home until ~%s before drawing from the grid.", currentStatus.BatterySOC, currentEnergyKWH, deficitStr)
		}
		return title, body

	case "executive":
		title := fmt.Sprintf("🌙 %.1f kWh Solar Today • 🔋 %.0f%% SOC", todayActualSolarKWH, currentStatus.BatterySOC)
		var body string
		if todayGridExportKWH > 0.5 {
			body = fmt.Sprintf("Solar generated %.1f kWh today with %.1f kWh exported to the grid. Battery entering night at %.0f%%.", todayActualSolarKWH, todayGridExportKWH, currentStatus.BatterySOC)
		} else if todayHomeUsageKWH > 0 && todayActualSolarKWH >= todayHomeUsageKWH {
			body = fmt.Sprintf("Solar generated %.1f kWh today, fully covering home needs. Battery entering night at %.0f%%.", todayActualSolarKWH, currentStatus.BatterySOC)
		} else if todayHomeUsageKWH > 0 && todayActualSolarKWH > 0.5 {
			coveragePct := (todayActualSolarKWH / todayHomeUsageKWH) * 100.0
			body = fmt.Sprintf("Solar generated %.1f kWh today (covered %.0f%% of home use). Battery entering night at %.0f%%.", todayActualSolarKWH, coveragePct, currentStatus.BatterySOC)
		} else {
			body = fmt.Sprintf("Home used %.1f kWh today with minimal solar. Battery entering night at %.0f%%.", todayHomeUsageKWH, currentStatus.BatterySOC)
		}
		return title, body

	case "pilot":
		title := "🤖 RateRudder: Evening Wrap-up"
		var body string
		if hitDeficitAt.IsZero() {
			if todayActualSolarKWH >= 2.0 {
				body = fmt.Sprintf("Automated battery managed %.1f kWh solar today. Stored %.1f kWh projected to power home through sunrise.", todayActualSolarKWH, currentEnergyKWH)
			} else {
				body = fmt.Sprintf("Battery maintained %.1f kWh reserve on a low solar day. Stored energy projected to power home through sunrise.", currentEnergyKWH)
			}
		} else {
			if todayActualSolarKWH >= 2.0 {
				body = fmt.Sprintf("Automated battery managed %.1f kWh solar today. Stored %.1f kWh will supply home until ~%s before switching to grid.", todayActualSolarKWH, currentEnergyKWH, deficitStr)
			} else {
				body = fmt.Sprintf("Stored %.1f kWh will supply home until ~%s before switching to grid.", currentEnergyKWH, deficitStr)
			}
		}
		return title, body

	case "metrics_heavy":
		fallthrough
	default:
		title := fmt.Sprintf("🌙 %.1f kWh Solar • 🔋 %.0f%% SOC (%.1f kWh)", todayActualSolarKWH, currentStatus.BatterySOC, currentEnergyKWH)
		var body string
		var flowStr string
		if todayGridExportKWH > 0.0 {
			flowStr = fmt.Sprintf("Today: %.1f kWh solar, %.1f kWh home (%.1f kWh exported).", todayActualSolarKWH, todayHomeUsageKWH, todayGridExportKWH)
		} else if todayGridImportKWH > 0.0 {
			flowStr = fmt.Sprintf("Today: %.1f kWh solar, %.1f kWh home (%.1f kWh imported).", todayActualSolarKWH, todayHomeUsageKWH, todayGridImportKWH)
		} else {
			flowStr = fmt.Sprintf("Today: %.1f kWh solar, %.1f kWh home.", todayActualSolarKWH, todayHomeUsageKWH)
		}

		if hitDeficitAt.IsZero() {
			body = fmt.Sprintf("%s Battery: %.1f kWh powers home through sunrise.", flowStr, currentEnergyKWH)
		} else {
			body = fmt.Sprintf("%s Battery: %.1f kWh powers home until ~%s.", flowStr, currentEnergyKWH, deficitStr)
		}
		return title, body
	}
}

// notificationsEnabled returns true if VAPID keys are properly configured.
func (s *Server) notificationsEnabled() bool {
	return s.vapidKey != nil && s.vapidPublicKey != ""
}

// pushPayload defines the JSON payload structure sent inside the Web Push body.
type pushPayload struct {
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Tag   string         `json:"tag,omitempty"`
	Icon  string         `json:"icon,omitempty"`
	Badge string         `json:"badge,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// encryptWebPushPayload encrypts a plaintext message for a subscriber using RFC 8291 (aes128gcm).
// Peer public keys must be 65 bytes in uncompressed EC point format (ANSI X9.62 / SEC 1: 0x04 prefix byte followed by 32 bytes X and 32 bytes Y coordinate).
func encryptWebPushPayload(plaintext []byte, peerPublicKeyBase64, peerAuthBase64 string) ([]byte, string, error) {
	peerPubBytes, err := decodeBase64Key(peerPublicKeyBase64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode subscriber public key: %w", err)
	}
	// SEC 1 uncompressed point format: 1 byte header (0x04) + 32 bytes X + 32 bytes Y = 65 bytes
	if len(peerPubBytes) != 65 || peerPubBytes[0] != 0x04 {
		return nil, "", fmt.Errorf("invalid subscriber public key: expected 65-byte uncompressed point")
	}

	peerAuthBytes, err := decodeBase64Key(peerAuthBase64)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode subscriber auth secret: %w", err)
	}

	peerPub, err := ecdh.P256().NewPublicKey(peerPubBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse subscriber public key: %w", err)
	}

	// 1. Generate local ephemeral ECDH keypair
	localPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate ephemeral keypair: %w", err)
	}
	localPubBytes := localPriv.PublicKey().Bytes()

	// 2. Compute shared secret
	sharedSecret, err := localPriv.ECDH(peerPub)
	if err != nil {
		return nil, "", fmt.Errorf("failed to compute shared ECDH secret: %w", err)
	}

	// 3. Generate 16-byte random salt
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	// 4. Derive PRK using peerAuthBytes as salt and sharedSecret as IKM
	prkHKDF := hkdf.Extract(sha256.New, sharedSecret, peerAuthBytes)

	// Derive IKM
	var keyInfo bytes.Buffer
	keyInfo.WriteString("WebPush: info\x00")
	keyInfo.Write(peerPubBytes)
	keyInfo.Write(localPubBytes)

	ikmReader := hkdf.Expand(sha256.New, prkHKDF, keyInfo.Bytes())
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(ikmReader, ikm); err != nil {
		return nil, "", fmt.Errorf("failed to derive IKM: %w", err)
	}

	// 5. Derive Content Encryption Key (CEK) and Nonce
	prk := hkdf.Extract(sha256.New, ikm, salt)
	encoding := "aes128gcm"

	cekInfo := []byte("Content-Encoding: " + encoding + "\x00")
	cekReader := hkdf.Expand(sha256.New, prk, cekInfo)
	cek := make([]byte, 16)
	if _, err := io.ReadFull(cekReader, cek); err != nil {
		return nil, "", fmt.Errorf("failed to derive CEK: %w", err)
	}

	nonceInfo := []byte("Content-Encoding: nonce\x00")
	nonceReader := hkdf.Expand(sha256.New, prk, nonceInfo)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(nonceReader, nonce); err != nil {
		return nil, "", fmt.Errorf("failed to derive nonce: %w", err)
	}

	// 6. Encrypt with AES-GCM-128
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Payload plaintext padding: append delimiter byte 0x02
	paddedPlaintext := append(plaintext, 0x02)
	ciphertext := gcm.Seal(nil, nonce, paddedPlaintext, nil)

	// 7. Construct RFC 8188 header
	// Header format:
	// salt (16 bytes) || rs (4 bytes) || idlen (1 byte) || keyid (65 bytes) || ciphertext
	const recordSize uint32 = 4096
	var buf bytes.Buffer
	buf.Write(salt)
	// binary.Write to an in-memory bytes.Buffer never returns an error, so ignoring error is safe.
	_ = binary.Write(&buf, binary.BigEndian, recordSize)
	buf.WriteByte(byte(len(localPubBytes)))
	buf.Write(localPubBytes)
	buf.Write(ciphertext)

	return buf.Bytes(), "aes128gcm", nil
}

// createVAPIDToken generates a signed JWT token for VAPID authorization (RFC 8292).
// audience specifies the push service origin URI (e.g. "https://fcm.googleapis.com").
// subject specifies the contact URI (mailto: or https:) for the application server admin.
func createVAPIDToken(privKey *ecdsa.PrivateKey, audience string, subject string, expiration time.Time) (string, error) {
	header := map[string]string{
		"typ": "JWT",
		"alg": "ES256",
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	claims := map[string]any{
		"aud": audience,
		"exp": expiration.Unix(),
		"sub": subject,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64
	hash := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("failed to sign VAPID token: %w", err)
	}

	// Format signature as IEEE P1363 (32 bytes R || 32 bytes S)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)
	return signingInput + "." + sigB64, nil
}

// sendWebPush sends an encrypted Web Push notification using RFC 8291 and RFC 8292.
func (s *Server) sendWebPush(
	ctx context.Context,
	sub types.PushSubscription,
	payload pushPayload,
	ttlSeconds int,
	urgency string,
) (int, error) {
	if !s.notificationsEnabled() {
		return 0, errors.New("notifications are not enabled on server")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal push payload: %w", err)
	}

	encryptedBody, encoding, err := encryptWebPushPayload(payloadBytes, sub.Keys.P256DH, sub.Keys.Auth)
	if err != nil {
		return 0, fmt.Errorf("failed to encrypt push payload: %w", err)
	}

	parsedEndpoint, err := url.Parse(sub.Endpoint)
	if err != nil {
		return 0, fmt.Errorf("invalid push subscription endpoint: %w", err)
	}
	audience := fmt.Sprintf("%s://%s", parsedEndpoint.Scheme, parsedEndpoint.Host)

	// RFC 8292 Section 2 mandates that VAPID expiration must not be more than 24 hours in the future.
	// 12 hours provides sufficient leeway for network retries while complying with RFC requirements.
	vapidToken, err := createVAPIDToken(s.vapidKey, audience, s.vapidSubject, s.now().Add(12*time.Hour))
	if err != nil {
		return 0, fmt.Errorf("failed to generate VAPID token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(encryptedBody))
	if err != nil {
		return 0, fmt.Errorf("failed to create push request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", encoding)
	req.Header.Set("TTL", fmt.Sprintf("%d", ttlSeconds))
	if urgency != "" {
		req.Header.Set("Urgency", urgency)
	}

	// Support both RFC 8292 'vapid' and modern 'WebPush' schemes
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", vapidToken, s.vapidPublicKey))
	req.Header.Set("Crypto-Key", fmt.Sprintf("p256ecdsa=%s", s.vapidPublicKey))

	client := common.HTTPClient(10 * time.Second)
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	return res.StatusCode, nil
}

// generateNotificationLogID generates a unique ID for a notification dispatch encoding month and siteID.
// Format: <month>_<siteID>_<hash> (e.g., 2026-09_mysite_a1b2c3d4e5f67890).
func generateNotificationLogID(siteID, userID, endpoint string, ts time.Time) string {
	month := ts.UTC().Format("2006-01")
	h := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%s:%d", siteID, userID, endpoint, ts.UnixNano()))
	return fmt.Sprintf("%s_%s_%s", month, siteID, hex.EncodeToString(h[:8]))
}

// parseNotificationLogID extracts month, siteID, and hash from a structured notification ID (<month>_<siteID>_<hash>).
func parseNotificationLogID(id string) (month, siteID, hash string, err error) {
	// Minimum length: 7 (YYYY-MM) + 1 (_) + 1 (siteID) + 1 (_) + 16 (hash) = 26
	if len(id) < 26 || id[7] != '_' {
		return "", "", "", fmt.Errorf("invalid notification ID structure: %s", id)
	}
	month = id[:7]
	if month[4] != '-' {
		return "", "", "", fmt.Errorf("invalid month in notification ID: %s", id)
	}
	lastUnderscore := strings.LastIndex(id, "_")
	if lastUnderscore <= 7 {
		return "", "", "", fmt.Errorf("invalid notification ID delimiters: %s", id)
	}
	siteID = id[8:lastUnderscore]
	hash = id[lastUnderscore+1:]
	if siteID == "" || hash == "" {
		return "", "", "", fmt.Errorf("missing siteID or hash in notification ID: %s", id)
	}
	return month, siteID, hash, nil
}

type siteRecentNotifications struct {
	logs []types.NotificationLog
}

func (n *siteRecentNotifications) hasSentToday(userID, notifType, dateStr string, loc *time.Location) bool {
	if loc == nil {
		loc = time.UTC
	}
	for _, l := range n.logs {
		if l.UserID == userID && l.Type == notifType && l.Success {
			if l.TSCreated.In(loc).Format("2006-01-02") == dateStr {
				return true
			}
		}
	}
	return false
}

func (n *siteRecentNotifications) hasSentWithin(userID, notifType string, d time.Duration, now time.Time) bool {
	cutoff := now.Add(-d)
	for _, l := range n.logs {
		if l.UserID == userID && l.Type == notifType && l.Success {
			if l.TSCreated.After(cutoff) {
				return true
			}
		}
	}
	return false
}

func (n *siteRecentNotifications) lastGridEvent() string {
	var latest time.Time
	var eventType string
	for _, l := range n.logs {
		if (l.Type == types.NotificationTypeGridOutage || l.Type == types.NotificationTypeGridRestored) && l.Success {
			if l.TSCreated.After(latest) {
				latest = l.TSCreated
				eventType = l.Type
			}
		}
	}
	return eventType
}

func (n *siteRecentNotifications) lastPriceSpikeLog(userID string) *types.NotificationLog {
	var latest time.Time
	var latestLog *types.NotificationLog
	for i := range n.logs {
		l := &n.logs[i]
		if l.UserID == userID && l.Type == types.NotificationTypePriceSpike && l.Success {
			if l.TSCreated.After(latest) {
				latest = l.TSCreated
				latestLog = l
			}
		}
	}
	return latestLog
}

func extractHighestAlertedPrice(l *types.NotificationLog) float64 {
	if l == nil || len(l.Metadata) == 0 {
		return 0
	}
	var maxP float64
	if peakStr, ok := l.Metadata["peakPrice"]; ok && peakStr != "" {
		if p, err := strconv.ParseFloat(peakStr, 64); err == nil && p > maxP {
			maxP = p
		}
	}
	if priceStr, ok := l.Metadata["price"]; ok && priceStr != "" {
		if p, err := strconv.ParseFloat(priceStr, 64); err == nil && p > maxP {
			maxP = p
		}
	}
	return maxP
}

// priceDroppedBelowBetween checks if electricity prices dropped below spike threshold conditions
// at any point between the previous alert timestamp (start) and the start of the candidate spike (end).
//
// If prices dropped back to normal levels in between, the user is eligible for a re-alert
// after the 6-hour cooldown window. If prices remained continuously elevated without dropping below,
// repeated alerts for the same ongoing elevated price event are suppressed until the next day (24h).
func priceDroppedBelowBetween(
	histPrices []types.Price,
	start, end time.Time,
	rawCosts []float64,
	reqPercentile, minDelta float64,
	useTimeOfDayRelative bool,
	loc *time.Location,
) bool {
	if loc == nil {
		loc = time.UTC
	}
	pctVal := computePricePercentile(rawCosts, reqPercentile)
	medianCost := computePricePercentile(rawCosts, 0.50)
	foundAny := false
	for _, p := range histPrices {
		if p.TSStart.After(start) && p.TSStart.Before(end) {
			foundAny = true
			cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
			var ref float64
			if useTimeOfDayRelative {
				ref = computeTimeOfDayRefPrice(histPrices, p.TSStart, loc, medianCost)
			} else {
				ref = medianCost
			}
			// If at any hour the cost was below the absolute floor, below the percentile threshold,
			// or failed to exceed the reference price by the required delta, it is considered dropped below.
			if cost < priceSpikeAbsoluteFloorDollarsPerKWH || cost < pctVal || (cost-ref) < minDelta {
				return true
			}
		}
	}
	// If no prices were found in the window, default to true to allow alerting
	if !foundAny {
		return true
	}
	return false
}

// computePricePercentile returns the p-th percentile value (e.g. p=0.90 for 90th percentile)
// from a slice of prices using nearest-rank selection over a sorted copy.
func computePricePercentile(prices []float64, p float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	sorted := make([]float64, len(prices))
	copy(sorted, prices)
	sort.Float64s(sorted)
	idx := int(float64(len(sorted)-1) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// computeTimeOfDayRefPrice calculates the typical baseline price for a target time of day,
// using historical prices from previous days within a +/- 1 hour window.
//
// For example, if targetTime is 7:00 PM, this function inspects historical prices at 6:00 PM,
// 7:00 PM, and 8:00 PM across past days. The +/- 1 hour buffer accounts for slight shifts in peak
// hours, seasonal daylight changes, and Daylight Saving Time adjustments.
//
// We use the median (50th percentile) of this 3-hour window across previous days rather than the maximum:
//  1. Normal recurring daily peaks around this time form the baseline (so regular daily peaks don't alert).
//  2. An occasional past price spike in this window does NOT artificially inflate the baseline or poison
//     future alerts (unlike a maximum, a few spike hours won't move the median).
//  3. If there are no historical prices in this window, it gracefully falls back to the provided fallback
//     (typically the all-hours median).
func computeTimeOfDayRefPrice(histPrices []types.Price, targetTime time.Time, loc *time.Location, fallback float64) float64 {
	if loc == nil {
		loc = time.UTC
	}
	targetLocal := targetTime.In(loc)
	targetHour := targetLocal.Hour()
	targetY, targetM, targetD := targetLocal.Date()

	// 3-hour window around targetHour (+/- 1 hour), with modulo 24 handling midnight wrap-around cleanly
	prevHour := (targetHour + 23) % 24
	nextHour := (targetHour + 1) % 24

	var windowCosts []float64
	for _, p := range histPrices {
		pLocal := p.TSStart.In(loc)
		pY, pM, pD := pLocal.Date()

		// Only inspect previous days, never prices from the current target day
		if pY == targetY && pM == targetM && pD == targetD {
			continue
		}

		h := pLocal.Hour()
		if h == prevHour || h == targetHour || h == nextHour {
			cost := p.DollarsPerKWH + p.GridUseDollarsPerKWH
			windowCosts = append(windowCosts, cost)
		}
	}

	if len(windowCosts) == 0 {
		return fallback
	}

	// Use median of prices in this time window as the representative baseline
	return computePricePercentile(windowCosts, 0.50)
}

// notificationTag returns the push notification tag for a given notification type and site.
// Solar underproduction, grid outages, grid restored, and daily summaries share the primary
// tag ("raterudder-" + siteID) so that alerts overwrite summaries on the user's device when
// conditions change. Price spike and VPP dispatch alerts use distinct tags so they do not
// overwrite summaries or each other.
func notificationTag(notifType string, siteID string) string {
	switch notifType {
	case types.NotificationTypePriceSpike:
		return "raterudder-" + siteID + "-price-spike"
	case types.NotificationTypeVPPDispatch:
		return "raterudder-" + siteID + "-vpp"
	default:
		return "raterudder-" + siteID
	}
}

// dispatchPushToUser sends a push notification to all subscriptions of a user, handles dead subscription pruning, and logs.
func (s *Server) dispatchPushToUser(
	ctx context.Context,
	siteID string,
	user types.User,
	notifType string,
	flavor string,
	title string,
	body string,
	urlPath string,
	metadata map[string]string,
) {
	nowUTC := s.now().UTC()
	for _, sub := range user.Subscriptions {
		logID := generateNotificationLogID(siteID, user.ID, sub.Endpoint, nowUTC)
		payload := pushPayload{
			Title: title,
			Body:  body,
			Tag:   notificationTag(notifType, siteID),
			Icon:  "/logo_192.png",
			Badge: "/badge_96.png",
			Data: map[string]any{
				"url":      urlPath,
				"id":       logID,
				"logID":    logID,
				"metadata": metadata,
			},
		}

		statusCode, err := s.sendWebPush(ctx, sub, payload, 14400, "normal")
		success := err == nil && (statusCode >= 200 && statusCode < 300)

		var errStr string
		if err != nil {
			errStr = err.Error()
		}

		// Prune dead subscriptions on 404 or 410
		if statusCode == http.StatusNotFound || statusCode == http.StatusGone {
			log.Ctx(ctx).InfoContext(ctx, "pruning dead push subscription",
				slog.String("userID", user.ID),
				slog.String("endpoint", sub.Endpoint),
				slog.Int("statusCode", statusCode),
			)
			if rmErr := s.storage.RemoveUserPushSubscription(ctx, user.ID, sub.Endpoint); rmErr != nil {
				log.Ctx(ctx).WarnContext(ctx, "failed to remove dead push subscription", slog.Any("error", rmErr))
			}
		}

		// Append log to monthly document
		logEntry := types.NotificationLog{
			ID:         logID,
			TSCreated:  nowUTC,
			UserID:     user.ID,
			Endpoint:   sub.Endpoint,
			Type:       notifType,
			Flavor:     flavor,
			Title:      title,
			Body:       body,
			Success:    success,
			StatusCode: statusCode,
			Error:      errStr,
			Metadata:   metadata,
		}

		if appendErr := s.storage.AppendNotificationLog(ctx, siteID, logEntry); appendErr != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to append notification log",
				slog.String("siteID", siteID),
				slog.String("userID", user.ID),
				slog.String("type", notifType),
				slog.Any("error", appendErr),
			)
		}
	}
}

// getSiteRecentNotifications returns recent notification logs for the past 24 hours wrapped in siteRecentNotifications.
func (s *Server) getSiteRecentNotifications(ctx context.Context, siteID string, nowLocal time.Time) *siteRecentNotifications {
	logs, err := s.storage.GetNotificationLogs(ctx, siteID, nowLocal.Add(-24*time.Hour), nowLocal.Add(1*time.Hour))
	if err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to get recent notification logs", slog.String("siteID", siteID), slog.Any("error", err))
	}
	return &siteRecentNotifications{logs: logs}
}

// newSiteRecentNotificationsFetcher returns a lazy fetcher that retrieves and caches recent notification logs on first call.
func (s *Server) newSiteRecentNotificationsFetcher(ctx context.Context, siteID string, nowLocal time.Time) func() *siteRecentNotifications {
	var (
		fetched bool
		state   *siteRecentNotifications
	)
	return func() *siteRecentNotifications {
		if !fetched {
			state = s.getSiteRecentNotifications(ctx, siteID, nowLocal)
			fetched = true
		}
		return state
	}
}

type dataForNotifications struct {
	settings       types.Settings
	essSystem      ess.System
	currentPrice   types.Price
	status         types.SystemStatus
	vppInfo        types.UtilityVPPInfo
	energyHistory  []types.DailyEnergyStats
	weatherHistory []types.Weather
	futurePrices   []types.Price
	simData        []controller.SimHour
	simDataL       sync.Mutex
}

// this is kind of gross that we
func (d *dataForNotifications) getSimData(ctx context.Context, s *Server, siteID string, nowLocal time.Time) []controller.SimHour {
	d.simDataL.Lock()
	defer d.simDataL.Unlock()
	if d.simData == nil {
		flatEnergyHistory := flattenDailyEnergyStats(d.energyHistory)
		simData, _ := s.controller.SimulateState(ctx, nowLocal, d.status, d.currentPrice, d.futurePrices, flatEnergyHistory, d.weatherHistory, d.settings)
		d.simData = simData
	}
	return d.simData
}

// handleNotifications evaluates and dispatches any due notifications during the site update cycle.
func (s *Server) handleNotifications(
	ctx context.Context,
	siteID string,
	data *dataForNotifications,
) {
	if !s.notificationsEnabled() {
		return
	}

	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	nowLocal := s.now().In(siteLoc)

	site, err := s.storage.GetSite(ctx, siteID)
	if err != nil || len(site.Notifications) == 0 {
		return
	}
	if site.ID == "" {
		site.ID = siteID
	}

	wg := new(sync.WaitGroup)

	getNotifState := s.newSiteRecentNotificationsFetcher(ctx, siteID, nowLocal)

	// 1. Morning Summary
	wg.Go(func() {
		s.handleMorningSummaryNotifications(ctx, site, data, nowLocal, getNotifState)
	})

	// 2. Evening Summary
	wg.Go(func() {
		s.handleEveningSummaryNotifications(ctx, site, data, nowLocal, getNotifState)
	})

	// 3. Grid Restoration & Outage
	wg.Go(func() {
		s.handleGridOutageNotifications(ctx, site, data.status, data.essSystem, getNotifState)
	})

	// 4. Real-Time Price Spike
	wg.Go(func() {
		s.handlePriceSpikeNotifications(ctx, site, data, nowLocal, getNotifState)
	})

	// 5. Unexpected Solar Underproduction
	wg.Go(func() {
		s.handleSolarUnderproductionNotifications(ctx, site, data, nowLocal, getNotifState)
	})

	// 6. Unplanned VPP Dispatch
	wg.Go(func() {
		s.handleVPPDispatchNotifications(ctx, site, data.status, data.vppInfo, nowLocal, getNotifState)
	})

	wg.Wait()
}

// handleMorningSummaryNotifications evaluates and sends morning summary push notifications.
// It summarizes the day's solar production outlook, projected battery charging milestones (e.g.
// full battery ETA), and provides tailored advice based on the user's chosen flavor.
func (s *Server) handleMorningSummaryNotifications(
	ctx context.Context,
	site types.Site,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
) {
	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	todayDateStr := nowLocal.Format("2006-01-02")
	todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, siteLoc)
	todayEnd := todayStart.AddDate(0, 0, 1)

	// Calculate historical daily solar totals for days strictly before today.
	// Used to determine the historical peak solar generation day and yesterday's actual solar generation.
	dailySolarMap := make(map[string]float64)
	for _, day := range data.energyHistory {
		if !day.TSDayStart.IsZero() {
			dayStartLocal := day.TSDayStart.In(siteLoc)
			if dayStartLocal.Before(todayStart) {
				dayKey := dayStartLocal.Format("2006-01-02")
				if _, ok := dailySolarMap[dayKey]; !ok {
					dailySolarMap[dayKey] = 0
				}
			}
		}
		for _, h := range day.Hourly {
			tHour := h.TSHourStart.In(siteLoc)
			if tHour.Before(todayStart) {
				dayKey := tHour.Format("2006-01-02")
				dailySolarMap[dayKey] += h.SolarKWH
			}
		}
	}

	// Cold-start rule: if fewer than 3 days of historical data exist, do not generate morning summaries
	// because baseline solar ratios and yesterday comparisons cannot be reliably computed.
	if len(dailySolarMap) < 3 {
		return
	}

	var peakSolarKWH float64
	for _, kwh := range dailySolarMap {
		if kwh > peakSolarKWH {
			peakSolarKWH = kwh
		}
	}

	// Iterate through all users configured for this site and dispatch their morning summary
	for userID, notifConfig := range site.Notifications {
		if !notifConfig.MorningSummaryEnabled || nowLocal.Hour() != notifConfig.MorningSummaryHour {
			continue
		}
		// Ensure only one morning summary is delivered per user per calendar day
		if getNotifState != nil && getNotifState().hasSentToday(userID, types.NotificationTypeMorningSummary, todayDateStr, siteLoc) {
			continue
		}
		user, err := s.storage.GetUser(ctx, userID)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get user for morning summary notification",
				slog.String("siteID", site.ID),
				slog.String("userID", userID),
				slog.Any("error", err),
			)
			continue
		}
		if len(user.Subscriptions) == 0 {
			continue
		}

		// Retrieve simulation hourly projection data for today
		simData := data.getSimData(ctx, s, site.ID, nowLocal)

		var todayForecastKWH float64
		var hitCapacityAt time.Time
		maxSimSOC := data.status.BatterySOC

		// Aggregate today's forecasted solar generation, find the projected peak SOC,
		// and inspect when the battery is projected to hit 100% capacity.
		for _, slot := range simData {
			if !slot.TS.Before(todayStart) && slot.TS.Before(todayEnd) {
				todayForecastKWH += slot.PredictedSolarKWH

				if slot.BatteryCapacityKWH > 0 {
					startSOC := (slot.StartBatteryKWH / slot.BatteryCapacityKWH) * 100.0
					endSOC := (slot.BatteryKWH / slot.BatteryCapacityKWH) * 100.0
					if startSOC > maxSimSOC {
						maxSimSOC = startSOC
					}
					if endSOC > maxSimSOC {
						maxSimSOC = endSOC
					}
				}

				// Strictly inspect HitCapacityAt from the simulation controller
				if hitCapacityAt.IsZero() && !slot.HitCapacityAt.IsZero() && slot.HitCapacityAt.After(nowLocal) {
					hitCapacityAt = slot.HitCapacityAt
				}
			}
		}

		yesterdayStart := todayStart.AddDate(0, 0, -1)
		yesterdayActualKWH := dailySolarMap[yesterdayStart.Format("2006-01-02")]

		solarRatio := 0.0
		if peakSolarKWH > 0 {
			solarRatio = todayForecastKWH / peakSolarKWH
		}

		// Populate structured metadata for client-side rendering and logging analysis
		metadata := map[string]string{
			"currentSOC":        fmt.Sprintf("%.1f", data.status.BatterySOC),
			"peakSOC":           fmt.Sprintf("%.1f", maxSimSOC),
			"solarRatio":        fmt.Sprintf("%.2f", solarRatio),
			"forecastSolarKWH":  fmt.Sprintf("%.2f", todayForecastKWH),
			"yesterdaySolarKWH": fmt.Sprintf("%.2f", yesterdayActualKWH),
		}
		if !hitCapacityAt.IsZero() {
			metadata["hitCapacityAt"] = hitCapacityAt.In(siteLoc).Format(time.RFC3339)
		}

		title, body := generateMorningSummary(notifConfig.MorningSummaryFlavor, data.status, todayForecastKWH, yesterdayActualKWH, peakSolarKWH, hitCapacityAt, maxSimSOC, siteLoc)
		s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypeMorningSummary, notifConfig.MorningSummaryFlavor, title, body, "/forecast", metadata)
	}
}

// handleEveningSummaryNotifications evaluates and sends evening summary push notifications.
// It summarizes today's actual performance (solar generation, home usage, grid import/export)
// and projects whether the battery will supply the home through the night or hit reserve.
func (s *Server) handleEveningSummaryNotifications(
	ctx context.Context,
	site types.Site,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
) {
	siteLoc := data.status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	todayDateStr := nowLocal.Format("2006-01-02")

	for userID, notifConfig := range site.Notifications {
		if !notifConfig.EveningSummaryEnabled || nowLocal.Hour() != notifConfig.EveningSummaryHour {
			continue
		}
		// Ensure only one evening summary is delivered per user per calendar day
		if getNotifState != nil && getNotifState().hasSentToday(userID, types.NotificationTypeEveningSummary, todayDateStr, siteLoc) {
			continue
		}
		user, err := s.storage.GetUser(ctx, userID)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get user for evening summary notification",
				slog.String("siteID", site.ID),
				slog.String("userID", userID),
				slog.Any("error", err),
			)
			continue
		}
		if len(user.Subscriptions) == 0 {
			continue
		}

		// Inspect simulation slots to determine if the battery will hit reserve overnight.
		// If a deficit is predicted after tomorrow's solar refilling begins, the battery successfully
		// powers the home through the entire night.
		simData := data.getSimData(ctx, s, site.ID, nowLocal)

		var hitDeficitAt time.Time
		var tomorrowSolarStart time.Time
		tomorrowDay := nowLocal.AddDate(0, 0, 1).Day()

		for _, slot := range simData {
			if slot.TS.In(siteLoc).Day() == tomorrowDay && slot.PredictedSolarKWH > 0.2 && tomorrowSolarStart.IsZero() {
				tomorrowSolarStart = slot.TS
			}
			// Strictly inspect HitDeficitAt without buffering or threshold heuristics
			if hitDeficitAt.IsZero() && !slot.HitDeficitAt.IsZero() && slot.HitDeficitAt.After(nowLocal) {
				hitDeficitAt = slot.HitDeficitAt
			}
		}

		// If deficit is predicted only after tomorrow's solar starts refilling the battery,
		// then the battery successfully powers the home throughout the entire night.
		if !tomorrowSolarStart.IsZero() && !hitDeficitAt.IsZero() && hitDeficitAt.After(tomorrowSolarStart) {
			hitDeficitAt = time.Time{}
		}

		// Aggregate today's energy metrics from midnight up to the current hour
		todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, siteLoc)
		todayEnd := todayStart.AddDate(0, 0, 1)
		var todayActualSolarKWH, todayHomeUsageKWH, todayGridImportKWH, todayGridExportKWH float64
		for _, day := range data.energyHistory {
			for _, h := range day.Hourly {
				tHour := h.TSHourStart.In(siteLoc)
				if !tHour.Before(todayStart) && tHour.Before(todayEnd) {
					todayActualSolarKWH += h.SolarKWH
					todayHomeUsageKWH += h.HomeKWH
					todayGridImportKWH += h.GridImportKWH
					todayGridExportKWH += h.GridExportKWH
				}
			}
		}

		minSOC := data.settings.MinBatterySOC

		// Populate structured metadata for evening summary
		metadata := map[string]string{
			"currentSOC":         fmt.Sprintf("%.1f", data.status.BatterySOC),
			"todaySolarKWH":      fmt.Sprintf("%.2f", todayActualSolarKWH),
			"todayHomeUsageKWH":  fmt.Sprintf("%.2f", todayHomeUsageKWH),
			"todayGridImportKWH": fmt.Sprintf("%.2f", todayGridImportKWH),
			"todayGridExportKWH": fmt.Sprintf("%.2f", todayGridExportKWH),
		}

		title, body := generateEveningSummary(notifConfig.EveningSummaryFlavor, data.status, todayActualSolarKWH, todayHomeUsageKWH, todayGridExportKWH, todayGridImportKWH, minSOC, hitDeficitAt, siteLoc)
		s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypeEveningSummary, notifConfig.EveningSummaryFlavor, title, body, "/dashboard", metadata)
	}
}

// handleGridOutageNotifications evaluates and sends grid outage and restoration notifications.
func (s *Server) handleGridOutageNotifications(
	ctx context.Context,
	site types.Site,
	status types.SystemStatus,
	essSystem ess.System,
	getNotifState func() *siteRecentNotifications,
) {
	hasAnyGridOutageUser := false
	for _, notifConfig := range site.Notifications {
		if notifConfig.GridOutageAlert {
			hasAnyGridOutageUser = true
			break
		}
	}
	if !hasAnyGridOutageUser {
		return
	}

	if !status.GridUnavailable {
		if getNotifState == nil || getNotifState().lastGridEvent() != types.NotificationTypeGridOutage {
			return
		}
		for userID, notifConfig := range site.Notifications {
			if !notifConfig.GridOutageAlert {
				continue
			}
			user, err := s.storage.GetUser(ctx, userID)
			if err != nil || len(user.Subscriptions) == 0 {
				continue
			}
			title := "✅ Grid Power Restored"
			body := "The electric grid is back online. Your system has safely resumed normal grid-tied operation."
			metadata := map[string]string{
				"currentSOC": fmt.Sprintf("%.1f", status.BatterySOC),
			}
			s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypeGridRestored, "", title, body, "/#dashboard", metadata)
		}
	} else if status.GridUnavailable && essSystem != nil {
		if getNotifState != nil && getNotifState().lastGridEvent() == types.NotificationTypeGridOutage {
			return
		}
		var outageUsers []types.User
		for userID, notifConfig := range site.Notifications {
			if !notifConfig.GridOutageAlert {
				continue
			}
			user, err := s.storage.GetUser(ctx, userID)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get user for grid outage notification",
					slog.String("siteID", site.ID),
					slog.String("userID", userID),
					slog.Any("error", err),
				)
				continue
			}
			if len(user.Subscriptions) > 0 {
				outageUsers = append(outageUsers, user)
			}
		}

		if len(outageUsers) > 0 {
			if wg := common.CtxWaitGroup(ctx); wg != nil {
				wg.Add(1)
				delay := s.gridOutageDelay
				if delay == 0 {
					delay = defaultGridOutageDelay
				}
				log.Ctx(ctx).DebugContext(ctx, "grid unavailable detected, launching outage verification",
					slog.String("siteID", site.ID),
					slog.Duration("delay", delay),
				)
				go func(ctx context.Context) {
					defer wg.Done()
					asyncCtx, cancel := context.WithTimeout(ctx, delay+30*time.Second)
					defer cancel()

					select {
					case <-time.After(delay):
					case <-asyncCtx.Done():
						return
					}

					currStatus, err := essSystem.GetStatus(asyncCtx)
					if err != nil {
						log.Ctx(asyncCtx).WarnContext(asyncCtx, "failed to re-check grid status after outage delay",
							slog.String("siteID", site.ID),
							slog.Any("error", err),
						)
						return
					}

					log.Ctx(asyncCtx).DebugContext(asyncCtx, "grid outage verification result",
						slog.String("siteID", site.ID),
						slog.Bool("stillDown", currStatus.GridUnavailable),
						slog.Float64("batterySOC", currStatus.BatterySOC),
					)

					if currStatus.GridUnavailable {
						hrsRemainingStr := ""
						if currStatus.HomeKW > 0.1 && currStatus.BatteryCapacityKWH > 0 {
							availKWH := currStatus.BatteryCapacityKWH * (currStatus.BatterySOC / 100.0)
							hrs := availKWH / currStatus.HomeKW
							hrsRemainingStr = fmt.Sprintf(" (~%.1f hours remaining)", hrs)
						}
						title := "⚠️ Grid Outage Detected"
						body := fmt.Sprintf("Utility grid power is currently down. Battery reserve is at %.0f%%%s.", currStatus.BatterySOC, hrsRemainingStr)
						metadata := map[string]string{
							"currentSOC": fmt.Sprintf("%.1f", currStatus.BatterySOC),
							"homeKW":     fmt.Sprintf("%.2f", currStatus.HomeKW),
						}
						for _, user := range outageUsers {
							s.dispatchPushToUser(asyncCtx, site.ID, user, types.NotificationTypeGridOutage, "", title, body, "/dashboard", metadata)
						}
					} else {
						log.Ctx(asyncCtx).InfoContext(asyncCtx, "grid outage was a temporary blip (<5m), suppressed notification",
							slog.String("siteID", site.ID),
						)
					}
				}(ctx)
			}
		}
	} else {
		// Grid is available: check if we should send a restored notification
		if getNotifState == nil || getNotifState().lastGridEvent() != types.NotificationTypeGridOutage {
			return
		}

		for userID, notifConfig := range site.Notifications {
			if !notifConfig.GridOutageAlert {
				continue
			}
			user, err := s.storage.GetUser(ctx, userID)
			if err != nil {
				log.Ctx(ctx).ErrorContext(ctx, "failed to get user for grid restored notification",
					slog.String("siteID", site.ID),
					slog.String("userID", userID),
					slog.Any("error", err),
				)
				continue
			}
			if len(user.Subscriptions) > 0 {
				title := "✅ Grid Power Restored"
				body := fmt.Sprintf("Grid electricity has reconnected. Battery is at %.0f%%. System returned to normal operation.", status.BatterySOC)
				metadata := map[string]string{
					"currentSOC": fmt.Sprintf("%.1f", status.BatterySOC),
				}
				s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypeGridRestored, "", title, body, "/#dashboard", metadata)
			}
		}
	}
}

// handlePriceSpikeNotifications evaluates incoming current and forecasted electricity prices against
// historical baselines to send price spike push notifications to subscribed users.
//
// Key features:
// - Evaluates both active real-time prices and upcoming horizon forecasts.
// - Supports High, Medium, and Low sensitivity tiers:
//   - High: Triggers on top 10% of all hours without time-of-day restriction.
//   - Medium: Triggers on top 10% of all hours AND requires price to exceed the +/- 1h time-of-day baseline.
//   - Low: Triggers on top 5% of all hours AND requires price to exceed the +/- 1h time-of-day baseline.
//
// - Scans forward to detect contiguous duration and projected peak time/price.
// - Formats battery simulation outcome (solar coverage, battery capacity survival, reserve deficit ETA).
// - Enforces a tiered anti-flapping cooldown: 1-hour lockout, 1-6h surge threshold, and 6-24h drop-below check.
func (s *Server) handlePriceSpikeNotifications(
	ctx context.Context,
	site types.Site,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
) {
	if data == nil || (len(data.futurePrices) == 0 && data.currentPrice.TSStart.IsZero()) {
		return
	}

	// Calculate total unit electricity cost ($/kWh) including energy supply and grid delivery fees
	currentCost := data.currentPrice.DollarsPerKWH + data.currentPrice.GridUseDollarsPerKWH
	var nextPriceCost float64
	var hasFuturePrice bool
	if len(data.futurePrices) > 0 {
		hasFuturePrice = true
		nextPriceCost = data.futurePrices[0].DollarsPerKWH + data.futurePrices[0].GridUseDollarsPerKWH
	}

	// Fast rejection: If neither the current price nor the next future price meets the absolute price floor,
	// skip history fetching and heavy evaluations entirely.
	currentCouldSpike := !data.currentPrice.TSStart.IsZero() && currentCost >= priceSpikeAbsoluteFloorDollarsPerKWH
	futureCouldSpike := hasFuturePrice && nextPriceCost >= priceSpikeAbsoluteFloorDollarsPerKWH
	if !currentCouldSpike && !futureCouldSpike {
		return
	}

	// Determine site location / timezone for day-of-week and hour-of-day evaluations
	var siteLoc *time.Location
	if !data.currentPrice.TSStart.IsZero() {
		siteLoc = data.currentPrice.TSStart.Location()
	} else if len(data.futurePrices) > 0 && !data.futurePrices[0].TSStart.IsZero() {
		siteLoc = data.futurePrices[0].TSStart.Location()
	} else if !nowLocal.IsZero() {
		siteLoc = nowLocal.Location()
	}
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	// Filter users configured to receive price spike alerts for this site
	var spikeUsers []struct {
		userID      string
		sensitivity string
	}
	for userID, notifConfig := range site.Notifications {
		if notifConfig.PriceSpikeAlert == "" || notifConfig.PriceSpikeAlert == "disabled" {
			continue
		}
		spikeUsers = append(spikeUsers, struct {
			userID      string
			sensitivity string
		}{userID: userID, sensitivity: notifConfig.PriceSpikeAlert})
	}
	if len(spikeUsers) == 0 {
		return
	}

	// Fetch up to 5 days of recent price history to establish the baseline percentile distributions
	startHist := nowLocal.AddDate(0, 0, -5).UTC()
	endHist := nowLocal.UTC()
	histPrices, err := s.storage.GetPriceHistory(ctx, site.ID, startHist, endHist)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get price history for price spike notification",
			slog.String("siteID", site.ID),
			slog.Any("error", err),
		)
		return
	}
	if len(histPrices) == 0 {
		return
	}

	// Compute raw total unit costs and overall median across all historical hours
	var rawCosts []float64
	for _, p := range histPrices {
		rawCosts = append(rawCosts, p.DollarsPerKWH+p.GridUseDollarsPerKWH)
	}
	medianCost := computePricePercentile(rawCosts, 0.50)

	for _, su := range spikeUsers {
		// Map user-selected sensitivity tier to spike detection parameters:
		// - "high": Top 10% (0.90) of all hours alone. Does NOT require time-of-day relative comparison,
		//   so it alerts whenever price is in the top 10% of all hours, even if recurring every week.
		// - "medium": Top 10% (0.90) of all hours AND time-of-day relative (+/- 1 hour buffer).
		//   Requires the price to be in the top 10% of all hours AND significantly above the typical
		//   price for this time of day (+/- 1h), filtering out normal daily evening peaks.
		// - "low": Top 5% (0.95) of all hours AND time-of-day relative (+/- 1 hour buffer).
		//   Only alerts on severe surges in the top 5% that also exceed the time-of-day baseline.
		var minDelta, reqPercentile, escalatedPercentile float64
		var useTimeOfDayRelative bool
		switch su.sensitivity {
		case "low":
			minDelta = priceSpikeMinDeltaLow
			reqPercentile = priceSpikePercentileLow
			escalatedPercentile = priceSpikeEscalatedPercentileLow
			useTimeOfDayRelative = true
		case "high":
			minDelta = priceSpikeMinDeltaHigh
			reqPercentile = priceSpikePercentileHigh
			escalatedPercentile = priceSpikeEscalatedPercentileHigh
			useTimeOfDayRelative = false
		case "medium":
			fallthrough
		default:
			minDelta = priceSpikeMinDeltaMedium
			reqPercentile = priceSpikePercentileMedium
			escalatedPercentile = priceSpikeEscalatedPercentileMedium
			useTimeOfDayRelative = true
		}

		pctVal := computePricePercentile(rawCosts, reqPercentile)

		// Candidate 1: Current price (active right now).
		// For Medium/Low, reference price is the time-of-day median (+/- 1 hour on previous days).
		// For High sensitivity, reference price is the overall all-hours median.
		var refPriceCurrent float64
		if useTimeOfDayRelative {
			refPriceCurrent = computeTimeOfDayRefPrice(histPrices, data.currentPrice.TSStart, siteLoc, medianCost)
		} else {
			refPriceCurrent = medianCost
		}
		isCurrentSpike := !data.currentPrice.TSStart.IsZero() &&
			currentCost >= priceSpikeAbsoluteFloorDollarsPerKWH &&
			currentCost >= pctVal &&
			(currentCost-refPriceCurrent) >= minDelta

		// Candidate 2: Future upcoming price (data.futurePrices[0]).
		var isFutureSpike bool
		var nextPrice types.Price
		var refPriceNext float64
		if len(data.futurePrices) > 0 {
			nextPrice = data.futurePrices[0]
			if useTimeOfDayRelative {
				refPriceNext = computeTimeOfDayRefPrice(histPrices, nextPrice.TSStart, siteLoc, medianCost)
			} else {
				refPriceNext = medianCost
			}
			isFutureSpike = nextPriceCost >= priceSpikeAbsoluteFloorDollarsPerKWH &&
				nextPriceCost >= pctVal &&
				(nextPriceCost-refPriceNext) >= minDelta
		}

		// If neither the active current price nor the next upcoming hour qualifies as a spike, skip.
		if !isCurrentSpike && !isFutureSpike {
			continue
		}

		var spikeStart, spikeEnd time.Time
		var activeSpikeCost, maxSpikeCost float64
		var maxSpikeTime time.Time
		var refPrice float64

		// Scan forward to determine the full contiguous duration of elevated prices and locate the peak price/time.
		if isCurrentSpike {
			spikeStart = data.currentPrice.TSStart
			spikeEnd = data.currentPrice.TSEnd
			if spikeEnd.IsZero() {
				spikeEnd = spikeStart.Add(time.Hour)
			}
			activeSpikeCost = currentCost
			maxSpikeCost = currentCost
			maxSpikeTime = spikeStart
			refPrice = refPriceCurrent

			// Scan forward across contiguous future prices that remain elevated
			for _, fp := range data.futurePrices {
				if !fp.TSStart.Before(spikeEnd) && (fp.TSStart.Equal(spikeEnd) || fp.TSStart.Sub(spikeEnd) <= 15*time.Minute) {
					fpCost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
					var fpRef float64
					if useTimeOfDayRelative {
						fpRef = computeTimeOfDayRefPrice(histPrices, fp.TSStart, siteLoc, medianCost)
					} else {
						fpRef = medianCost
					}
					if fpCost >= priceSpikeAbsoluteFloorDollarsPerKWH && fpCost >= pctVal && (fpCost-fpRef) >= minDelta {
						if !fp.TSEnd.IsZero() {
							spikeEnd = fp.TSEnd
						} else {
							spikeEnd = fp.TSStart.Add(time.Hour)
						}
						if fpCost > maxSpikeCost+0.005 {
							maxSpikeCost = fpCost
							maxSpikeTime = fp.TSStart
						}
					} else {
						break
					}
				}
			}
		} else {
			spikeStart = nextPrice.TSStart
			spikeEnd = nextPrice.TSEnd
			if spikeEnd.IsZero() {
				spikeEnd = spikeStart.Add(time.Hour)
			}
			activeSpikeCost = nextPriceCost
			maxSpikeCost = nextPriceCost
			maxSpikeTime = nextPrice.TSStart
			refPrice = refPriceNext

			// Scan forward across subsequent future prices
			for i := 1; i < len(data.futurePrices); i++ {
				fp := data.futurePrices[i]
				if !fp.TSStart.Before(spikeEnd) && (fp.TSStart.Equal(spikeEnd) || fp.TSStart.Sub(spikeEnd) <= 15*time.Minute) {
					fpCost := fp.DollarsPerKWH + fp.GridUseDollarsPerKWH
					var fpRef float64
					if useTimeOfDayRelative {
						fpRef = computeTimeOfDayRefPrice(histPrices, fp.TSStart, siteLoc, medianCost)
					} else {
						fpRef = medianCost
					}
					if fpCost >= priceSpikeAbsoluteFloorDollarsPerKWH && fpCost >= pctVal && (fpCost-fpRef) >= minDelta {
						if !fp.TSEnd.IsZero() {
							spikeEnd = fp.TSEnd
						} else {
							spikeEnd = fp.TSStart.Add(time.Hour)
						}
						if fpCost > maxSpikeCost+0.005 {
							maxSpikeCost = fpCost
							maxSpikeTime = fp.TSStart
						}
					} else {
						break
					}
				}
			}
		}

		// Cooldown and Anti-Flapping Logic:
		// 1. Hard 1-hour lockout: Never re-alert within 1 hour of an alert.
		// 2. 1h to 6h window: Only re-alert if price is significantly higher (>= 20% surge above previously
		//    alerted/warned peak price) AND meets the escalated percentile threshold (e.g. top 5% or 2%).
		// 3. 6h to 24h window: Re-alert if prices dropped back below spike thresholds between alerts.
		//    If prices stayed continuously elevated for 6+ hours at the same high level, do not re-alert
		//    about the same price until the next day.
		// 4. 24h+: Standard alert thresholds apply.
		if getNotifState != nil {
			lastLog := getNotifState().lastPriceSpikeLog(su.userID)
			if lastLog != nil {
				timeSince := s.now().Sub(lastLog.TSCreated)
				if timeSince < 1*time.Hour {
					continue
				}

				prevPeakCost := extractHighestAlertedPrice(lastLog)
				var isSignificantSurge bool
				if prevPeakCost > 0 {
					escalatedPctVal := computePricePercentile(rawCosts, escalatedPercentile)
					checkCost := activeSpikeCost
					if maxSpikeCost > checkCost {
						checkCost = maxSpikeCost
					}
					isSignificantSurge = (checkCost >= prevPeakCost*priceSpikeSignificantMultiplier) && (checkCost >= escalatedPctVal)
				}

				if timeSince < 6*time.Hour {
					if !isSignificantSurge {
						continue
					}
				} else if timeSince < 24*time.Hour {
					droppedBelow := priceDroppedBelowBetween(
						histPrices,
						lastLog.TSCreated,
						spikeStart,
						rawCosts,
						reqPercentile,
						minDelta,
						useTimeOfDayRelative,
						siteLoc,
					)
					if !droppedBelow && !isSignificantSurge {
						continue
					}
				}
			}
		}

		user, err := s.storage.GetUser(ctx, su.userID)
		if err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to get user for price spike notification",
				slog.String("siteID", site.ID),
				slog.String("userID", su.userID),
				slog.Any("error", err),
			)
			continue
		}
		if len(user.Subscriptions) == 0 {
			continue
		}

		// Construct Notification Title and Opening Sentence:
		// If the spike is active right now:
		// - If upcoming hours peak even higher (+2¢ or more), mention the current price and projected peak time/price.
		// - Otherwise, state the current price and duration.
		// If the spike begins in an upcoming hour:
		// - State when it begins, along with any projected higher peak time/price.
		var title, firstSentence string
		if isCurrentSpike {
			if maxSpikeCost >= activeSpikeCost+0.02 {
				title = fmt.Sprintf("🚨 Price Spike: $%.2f/kWh (peaking at $%.2f at %s)", activeSpikeCost, maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"))
				firstSentence = fmt.Sprintf("Price is $%.2f/kWh now and expected to rise to $%.2f/kWh at %s (lasting until %s).", activeSpikeCost, maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"), spikeEnd.In(siteLoc).Format("3:04 PM"))
			} else {
				title = fmt.Sprintf("🚨 Price Spike: $%.2f/kWh", activeSpikeCost)
				firstSentence = fmt.Sprintf("Price is $%.2f/kWh now and anticipated to last until %s.", activeSpikeCost, spikeEnd.In(siteLoc).Format("3:04 PM"))
			}
		} else {
			if maxSpikeCost >= activeSpikeCost+0.02 {
				title = fmt.Sprintf("🚨 Price Spike Ahead: $%.2f/kWh at %s (peaking at $%.2f at %s)", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"), maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"))
				firstSentence = fmt.Sprintf("Price is projected to reach $%.2f/kWh at %s and peak at $%.2f/kWh at %s (lasting until %s).", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"), maxSpikeCost, maxSpikeTime.In(siteLoc).Format("3:04 PM"), spikeEnd.In(siteLoc).Format("3:04 PM"))
			} else {
				title = fmt.Sprintf("🚨 Price Spike Ahead: $%.2f/kWh at %s", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"))
				firstSentence = fmt.Sprintf("Price is projected to reach $%.2f/kWh at %s (lasting until %s).", activeSpikeCost, spikeStart.In(siteLoc).Format("3:04 PM"), spikeEnd.In(siteLoc).Format("3:04 PM"))
			}
		}

		// Second sentence explains the baseline comparison and what RateRudder will do.
		// Defaults to stating RateRudder will prioritize battery power.
		secondSentence := fmt.Sprintf("Electricity rates are surging significantly above recent prices ($%.2f/kWh). RateRudder will prioritize battery power to protect your home.", refPrice)

		// Inspect hourly simulation data across the spike slots to provide specific home battery outcome guidance:
		// 1. If solar generation covers entire home consumption: inform user solar is powering home without drawing battery.
		// 2. If battery is already at or below reserve: warn that the home will draw from grid during the spike.
		// 3. If battery will run out of energy before the spike ends: provide the estimated time battery reaches reserve.
		// 4. If battery lasts through the entire spike: reassure user battery powers home through the whole event.
		currentSOC := data.status.BatterySOC
		simData := data.getSimData(ctx, s, site.ID, nowLocal)
		if len(simData) > 0 {
			var spikeSlots []controller.SimHour
			for i, slot := range simData {
				var slotEnd time.Time
				if i+1 < len(simData) && !simData[i+1].TS.IsZero() {
					slotEnd = simData[i+1].TS
				} else if i > 0 && !simData[i-1].TS.IsZero() {
					slotEnd = slot.TS.Add(slot.TS.Sub(simData[i-1].TS))
				} else if !spikeEnd.IsZero() && spikeEnd.After(slot.TS) {
					slotEnd = spikeEnd
				} else {
					slotEnd = slot.TS.Add(30 * time.Minute)
				}

				// Check if the simulation slot overlaps with the price spike interval [spikeStart, spikeEnd)
				if slotEnd.After(spikeStart) && slot.TS.Before(spikeEnd) {
					spikeSlots = append(spikeSlots, slot)
				}
			}

			if len(spikeSlots) > 0 {
				hasSolar := false
				solarCoversAll := true
				for _, slot := range spikeSlots {
					// Require meaningful solar generation (>= 0.5 kWh, i.e. 500Wh for 1h or 1kW rate for 30m)
					// to conclude solar actively covers the home rather than zero-load/dawn noise.
					if slot.PredictedSolarKWH >= 0.5 {
						hasSolar = true
					}
					// If net home load after solar exceeds 0.1 kWh in any slot, solar does not cover all demand.
					if slot.NetLoadSolarKWH > 0.1 {
						solarCoversAll = false
					}
				}

				if hasSolar && solarCoversAll {
					secondSentence = "Solar is projected to cover your home during the spike without drawing from the battery."
				} else {
					if currentSOC == 0 && spikeSlots[0].BatteryCapacityKWH > 0 {
						currentSOC = (spikeSlots[0].StartBatteryKWH / spikeSlots[0].BatteryCapacityKWH) * 100.0
					}
					reserveSOC := data.settings.MinBatterySOC
					if reserveSOC == 0 && spikeSlots[0].BatteryCapacityKWH > 0 && spikeSlots[0].BatteryReserveKWH > 0 {
						reserveSOC = (spikeSlots[0].BatteryReserveKWH / spikeSlots[0].BatteryCapacityKWH) * 100.0
					}

					if currentSOC <= reserveSOC {
						secondSentence = fmt.Sprintf("Battery is currently at %.0f%% reserve; your home will draw from the grid during the spike.", currentSOC)
					} else {
						var hitDeficitAt time.Time
						for _, slot := range spikeSlots {
							if !slot.HitDeficitAt.IsZero() && !slot.HitDeficitAt.Before(spikeStart) && slot.HitDeficitAt.Before(spikeEnd) {
								hitDeficitAt = slot.HitDeficitAt
								break
							}
						}

						if !hitDeficitAt.IsZero() {
							secondSentence = fmt.Sprintf("Battery is at %.0f%% and projected to reach reserve at ~%s before the spike ends.", currentSOC, hitDeficitAt.In(siteLoc).Format("3:04 PM"))
						} else {
							secondSentence = fmt.Sprintf("Battery is at %.0f%% and projected to power your home through the entire spike.", currentSOC)
						}
					}
				}
			}
		}

		// Store structured metadata on the notification log for debugging and re-alert evaluations
		metadata := map[string]string{
			"price":      fmt.Sprintf("%.4f", activeSpikeCost),
			"peakPrice":  fmt.Sprintf("%.4f", maxSpikeCost),
			"refPrice":   fmt.Sprintf("%.4f", refPrice),
			"currentSOC": fmt.Sprintf("%.1f", currentSOC),
			"spikeStart": spikeStart.In(siteLoc).Format(time.RFC3339),
			"spikeEnd":   spikeEnd.In(siteLoc).Format(time.RFC3339),
		}

		body := fmt.Sprintf("%s %s", firstSentence, secondSentence)
		s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypePriceSpike, su.sensitivity, title, body, "/forecast", metadata)
	}
}

// handleSolarUnderproductionNotifications evaluates and sends alerts when actual solar production
// drops significantly below weather-forecasted generation during peak production hours.
//
// Key safeguards:
// 1. Time window: Only evaluated during peak midday hours (11:00 AM - 3:00 PM local time).
// 2. Weather overcast suppression: If cloud cover exceeds 60%, cloud cover explains the deficit, so no alert is sent.
// 3. Active alarms/storms: Suppressed during severe weather or ESS hardware alarms.
// 4. Sensitivity tiers:
//   - Low: Triggers if actual < 50% of forecast (noticeable underproduction / dirty panels).
//   - Medium: Triggers if actual < 30% of forecast (moderate underproduction / partial string failure).
//   - High: Triggers if actual < 15% of forecast (severe underproduction / inverter tripped).
//
// 5. Absolute deficit requirement: Requires at least 2.5 kW generation shortfall to prevent micro-alerts.
func (s *Server) handleSolarUnderproductionNotifications(
	ctx context.Context,
	site types.Site,
	data *dataForNotifications,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
) {
	hasAnySolarUser := false
	for _, notifConfig := range site.Notifications {
		if notifConfig.SolarUnderproductionAlert != "" && notifConfig.SolarUnderproductionAlert != "disabled" {
			hasAnySolarUser = true
			break
		}
	}
	// Restrict evaluation to peak solar hours (11 AM to 3 PM) and suppress during active storms or alarms
	if !hasAnySolarUser || nowLocal.Hour() < 11 || nowLocal.Hour() > 15 || len(data.status.Storms) > 0 || len(data.status.Alarms) > 0 {
		return
	}

	// Check whether recent weather forecasts indicate heavy cloud cover for this hour
	isOvercast := false
	for _, w := range data.weatherHistory {
		for _, hw := range w.ForecastHours {
			hwLocal := hw.TSHourStart.In(nowLocal.Location())
			if hwLocal.Year() == nowLocal.Year() && hwLocal.Month() == nowLocal.Month() && hwLocal.Day() == nowLocal.Day() && hwLocal.Hour() == nowLocal.Hour() {
				if hw.CloudCoverPercent >= solarUnderproductionMaxCloudCoverPercent {
					isOvercast = true
				}
				break
			}
		}
		if isOvercast {
			break
		}
	}
	if isOvercast {
		return
	}

	// Locate the forecasted solar generation for the current hour from simulation data
	simData := data.getSimData(ctx, s, site.ID, nowLocal)

	var forecastKW float64
	for _, slot := range simData {
		if slot.TS.In(nowLocal.Location()).Hour() == nowLocal.Hour() {
			forecastKW = slot.PredictedSolarKWH
			break
		}
	}

	// Skip if the forecast for this hour was negligible (< 3.0 kW)
	if forecastKW < solarUnderproductionMinForecastKW {
		return
	}

	todayDateStr := nowLocal.Format("2006-01-02")
	actualKW := data.status.SolarKW
	for userID, notifConfig := range site.Notifications {
		if notifConfig.SolarUnderproductionAlert == "" || notifConfig.SolarUnderproductionAlert == "disabled" {
			continue
		}
		var ratio float64
		switch notifConfig.SolarUnderproductionAlert {
		case "low":
			ratio = solarUnderproductionRatioLow
		case "high":
			ratio = solarUnderproductionRatioHigh
		case "medium":
			fallthrough
		default:
			ratio = solarUnderproductionRatioMedium
		}

		// Check if actual generation is below the sensitivity ratio AND meets the minimum kW deficit
		if actualKW < ratio*forecastKW && (forecastKW-actualKW) >= solarUnderproductionMinDeficitKW {
			if getNotifState != nil && getNotifState().hasSentToday(userID, types.NotificationTypeSolarUnderproduction, todayDateStr, nowLocal.Location()) {
				continue
			}
			user, err := s.storage.GetUser(ctx, userID)
			if err == nil && len(user.Subscriptions) > 0 {
				title := "⚠️ Solar Underproduction Alert"
				body := fmt.Sprintf("Solar panels are generating %.1f kW, significantly below the %.1f kW forecast for this hour. Check your solar inverter or breakers.", actualKW, forecastKW)
				metadata := map[string]string{
					"currentSolarKW":  fmt.Sprintf("%.2f", actualKW),
					"forecastSolarKW": fmt.Sprintf("%.2f", forecastKW),
					"deficitKW":       fmt.Sprintf("%.2f", forecastKW-actualKW),
				}
				s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypeSolarUnderproduction, notifConfig.SolarUnderproductionAlert, title, body, "/dashboard", metadata)
			}
		}
	}
}

// handleVPPDispatchNotifications evaluates and sends alerts when an unexpected or unplanned Virtual Power Plant
// (VPP) grid support event is triggered on the user's battery system.
func (s *Server) handleVPPDispatchNotifications(
	ctx context.Context,
	site types.Site,
	status types.SystemStatus,
	vppInfo types.UtilityVPPInfo,
	nowLocal time.Time,
	getNotifState func() *siteRecentNotifications,
) {
	if !status.VPPActive {
		return
	}

	hasAnyVPPUser := false
	for _, notifConfig := range site.Notifications {
		if notifConfig.VPPDispatchAlert {
			hasAnyVPPUser = true
			break
		}
	}
	if !hasAnyVPPUser {
		return
	}

	siteLoc := status.Timestamp.Location()
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	if nowLocal.IsZero() {
		nowLocal = s.now().In(siteLoc)
	}

	// Check if this VPP dispatch was scheduled or known in advance.
	// We only send alerts for unscheduled / unplanned VPP dispatches so users aren't spammed during normal scheduled programs.
	isPlanned := false
	for _, p := range vppInfo.Mandatory {
		if contains, _, _ := p.Contains(nowLocal); contains {
			isPlanned = true
			break
		}
	}
	if !isPlanned {
		for _, ev := range status.VPPEvents {
			if (nowLocal.Equal(ev.TSStart) || nowLocal.After(ev.TSStart)) && nowLocal.Before(ev.TSEnd) {
				isPlanned = true
				break
			}
		}
	}
	if isPlanned {
		return
	}

	for userID, notifConfig := range site.Notifications {
		if !notifConfig.VPPDispatchAlert {
			continue
		}
		if getNotifState != nil && getNotifState().hasSentWithin(userID, types.NotificationTypeVPPDispatch, vppDispatchDeduplicationWindow, s.now()) {
			continue
		}
		user, err := s.storage.GetUser(ctx, userID)
		if err == nil && len(user.Subscriptions) > 0 {
			title := "⚡ Virtual Power Plant Active"
			body := "RateRudder detected an active VPP grid support event on your system."
			if status.BatteryKW > 0.1 {
				body = "Your battery is discharging to support the electric grid during an unscheduled VPP event."
			}
			metadata := map[string]string{
				"currentSOC": fmt.Sprintf("%.1f", status.BatterySOC),
				"batteryKW":  fmt.Sprintf("%.2f", status.BatteryKW),
			}
			s.dispatchPushToUser(ctx, site.ID, user, types.NotificationTypeVPPDispatch, "", title, body, "/dashboard", metadata)
		}
	}
}

// handleGetVAPIDPublicKey returns the server's derived VAPID public key as an application/octet-stream binary.
func (s *Server) handleGetVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if !s.notificationsEnabled() {
		writeJSONError(w, "notifications not configured on server", http.StatusServiceUnavailable)
		return
	}

	pubBytes, err := decodeBase64Key(s.vapidPublicKey)
	if err != nil {
		writeJSONError(w, "failed to decode public key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(pubBytes); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// subscribeRequest payload for registering a push subscription.
type subscribeRequest struct {
	Subscription types.PushSubscription `json:"subscription"`
	SendTest     bool                   `json:"sendTest,omitempty"`
}

// handleSubscribe registers a new push subscription for the authenticated user.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.notificationsEnabled() {
		writeJSONError(w, "notifications not configured on server", http.StatusServiceUnavailable)
		return
	}

	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Subscription.Endpoint == "" || req.Subscription.Keys.P256DH == "" || req.Subscription.Keys.Auth == "" {
		writeJSONError(w, "subscription endpoint and keys are required", http.StatusBadRequest)
		return
	}

	// Generate SHA256 ID from endpoint if empty
	if req.Subscription.ID == "" {
		h := sha256.Sum256([]byte(req.Subscription.Endpoint))
		req.Subscription.ID = hex.EncodeToString(h[:])
	}
	req.Subscription.TSCreated = s.now().UTC()

	ctx := r.Context()
	if err := s.storage.AddUserPushSubscription(ctx, userID, req.Subscription); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to save push subscription", slog.Any("error", err))
		writeJSONError(w, "failed to save subscription", http.StatusInternalServerError)
		return
	}

	// If sendTest is requested, dispatch an immediate test notification
	if req.SendTest {
		payload := pushPayload{
			Title: "RateRudder Notifications Enabled",
			Body:  "You will now receive energy updates and alerts for your system.",
			Tag:   "raterudder-test",
			Icon:  "/logo_192.png",
			Badge: "/badge_96.png",
			Data: map[string]any{
				"url": "/dashboard",
			},
		}
		if code, err := s.sendWebPush(ctx, req.Subscription, payload, 300, "high"); err != nil {
			log.Ctx(ctx).ErrorContext(ctx, "failed to send test welcome push notification",
				slog.String("userID", userID),
				slog.Int("statusCode", code),
				slog.Any("error", err),
			)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// unsubscribeRequest payload for removing a push subscription.
type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// handleUnsubscribe removes a push subscription for the authenticated user.
func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" {
		writeJSONError(w, "endpoint is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := s.storage.RemoveUserPushSubscription(ctx, userID, req.Endpoint); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to remove push subscription", slog.Any("error", err))
		writeJSONError(w, "failed to remove subscription", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// getNotificationSettingsResponse payload for fetching user notification settings and active push subscriptions.
type getNotificationSettingsResponse struct {
	Settings      types.UserNotificationSettings `json:"settings"`
	Subscriptions []types.PushSubscription       `json:"subscriptions"`
	VAPIDEnabled  bool                           `json:"vapidEnabled"`
}

// handleGetNotificationSettings returns the user's notification preferences for a site along with active push subscriptions.
func (s *Server) handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	siteID := s.getSiteID(r)
	if siteID == "" {
		writeJSONError(w, "siteID required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	settings := types.UserNotificationSettings{
		MorningSummaryHour:   7,
		MorningSummaryFlavor: defaultSummaryFlavor,
		EveningSummaryHour:   20,
		EveningSummaryFlavor: defaultSummaryFlavor,
	}

	site, err := s.storage.GetSite(ctx, siteID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get site for notification settings",
			slog.String("siteID", siteID),
			slog.Any("error", err),
		)
		writeJSONError(w, "failed to get site for notification settings", http.StatusInternalServerError)
		return
	}
	if userSettings, ok := site.Notifications[userID]; ok {
		settings = userSettings
		if settings.MorningSummaryFlavor == "" {
			settings.MorningSummaryFlavor = defaultSummaryFlavor
		}
		if settings.MorningSummaryHour == 0 && !settings.MorningSummaryEnabled {
			settings.MorningSummaryHour = 7
		}
		if settings.EveningSummaryFlavor == "" {
			settings.EveningSummaryFlavor = defaultSummaryFlavor
		}
		if settings.EveningSummaryHour == 0 && !settings.EveningSummaryEnabled {
			settings.EveningSummaryHour = 20
		}
	}

	u, err := s.storage.GetUser(ctx, userID)
	if err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to get user for notification settings",
			slog.String("userID", userID),
			slog.Any("error", err),
		)
		writeJSONError(w, "failed to get user for notification settings", http.StatusInternalServerError)
		return
	}

	resp := getNotificationSettingsResponse{
		Settings:      settings,
		Subscriptions: u.Subscriptions,
		VAPIDEnabled:  s.notificationsEnabled(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// updateNotificationSettingsRequest payload for updating user notification preferences on a site.
type updateNotificationSettingsRequest struct {
	SiteID   string                         `json:"siteID"`
	Settings types.UserNotificationSettings `json:"settings"`
}

// handleUpdateNotificationSettings updates user notification preferences on a site.
func (s *Server) handleUpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	user := s.getUser(r)
	if user.ID == "" {
		writeJSONError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req updateNotificationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	siteID := s.getSiteID(r)
	if siteID == "" {
		writeJSONError(w, "siteID is required", http.StatusBadRequest)
		return
	}

	if req.Settings.MorningSummaryHour < 0 || req.Settings.MorningSummaryHour > 23 {
		writeJSONError(w, "morning summary hour must be between 0 and 23", http.StatusBadRequest)
		return
	}
	if req.Settings.MorningSummaryEnabled && req.Settings.MorningSummaryFlavor == "" {
		writeJSONError(w, "morning summary flavor is required", http.StatusBadRequest)
		return
	}
	if req.Settings.MorningSummaryFlavor == "" {
		req.Settings.MorningSummaryFlavor = defaultSummaryFlavor
	}
	if req.Settings.EveningSummaryHour < 0 || req.Settings.EveningSummaryHour > 23 {
		writeJSONError(w, "evening summary hour must be between 0 and 23", http.StatusBadRequest)
		return
	}
	if req.Settings.EveningSummaryEnabled && req.Settings.EveningSummaryFlavor == "" {
		writeJSONError(w, "evening summary flavor is required", http.StatusBadRequest)
		return
	}
	if req.Settings.EveningSummaryFlavor == "" {
		req.Settings.EveningSummaryFlavor = defaultSummaryFlavor
	}
	switch req.Settings.PriceSpikeAlert {
	case "", "disabled", "low", "medium", "high":
	default:
		writeJSONError(w, "invalid price spike alert sensitivity", http.StatusBadRequest)
		return
	}
	switch req.Settings.SolarUnderproductionAlert {
	case "", "disabled", "low", "medium", "high":
	default:
		writeJSONError(w, "invalid solar underproduction alert sensitivity", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := s.storage.UpdateSiteNotificationSettings(ctx, siteID, userID, req.Settings); err != nil {
		log.Ctx(ctx).ErrorContext(ctx, "failed to update notification settings", slog.Any("error", err))
		writeJSONError(w, "failed to update notification settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// handleNotificationClick records click telemetry from the Service Worker.
func (s *Server) handleNotificationClick(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var siteID, month, logID string

	if id == "" {
		writeJSONError(w, "missing notification id", http.StatusBadRequest)
		return
	}
	m, sID, _, err := parseNotificationLogID(id)
	if err != nil {
		writeJSONError(w, "invalid notification id", http.StatusBadRequest)
		return
	}
	month = m
	siteID = sID
	logID = id

	ctx := r.Context()
	if err := s.storage.RecordNotificationClick(ctx, siteID, month, logID, s.now().UTC()); err != nil {
		log.Ctx(ctx).WarnContext(ctx, "failed to record notification click", slog.Any("error", err), slog.String("id", logID))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"recorded": true}); err != nil {
		panic(http.ErrAbortHandler)
	}
}
