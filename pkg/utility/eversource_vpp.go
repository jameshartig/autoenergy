package utility

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/levenlabs/go-lflag"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/log"
	"github.com/raterudder/raterudder/pkg/storage"
	"github.com/raterudder/raterudder/pkg/types"
	"golang.org/x/net/html"
	"golang.org/x/sync/singleflight"
)

const (
	// Eversource VPP defines on-peak hours as 12:00 PM to 8:00 PM (hours 12 to 20) on weekdays.
	eversourceVPPOnPeakStartHour = 12
	eversourceVPPOnPeakEndHour   = 20
)

type vppDailyRate struct {
	date         time.Time
	onPeakPrice  float64
	offPeakPrice float64
}

type baseEversourceVPP struct {
	vppHistoryURL string
	client        *http.Client
	db            storage.Database

	mu           sync.Mutex
	cachedPrices map[string][]types.Price // key: YYYY-MM-DD
	sfGroup      singleflight.Group
}

func configuredEversourceVPP(db storage.Database) *baseEversourceVPP {
	c := &baseEversourceVPP{
		client:       common.HTTPClient(time.Minute),
		cachedPrices: make(map[string][]types.Price),
		db:           db,
	}
	historyURL := lflag.String("eversource-vpp-history-url", "https://www.eversource.com/clp/vpp/vpphistory.aspx", "URL for Eversource VPP price history")

	lflag.Do(func() {
		c.vppHistoryURL = *historyURL
		if c.vppHistoryURL == "" {
			log.Ctx(context.Background()).Error("eversource-vpp-history-url is required")
			os.Exit(1)
		}
	})

	return c
}

// eversourceVPPPeriods generates the VPP periods for CT Eversource.
/*
Passive discharge events are limited to non-holiday weekdays during the Summer season (June,
July and August) only. Holidays excluded from Passive Dispatch events include:
Holidays for Passive Dispatch Events
Summer Independence Day July 4
Summer Juneteenth June 19
*/
func eversourceVPPPeriods(opts types.UtilityRateOptions, years []int) (types.UtilityVPPInfo, error) {
	if opts.VPPProgram != "ess-passive" {
		return types.UtilityVPPInfo{}, nil
	}

	var periods []types.UtilityVPPPeriod

	for _, year := range years {
		june19 := juneteenth(year)
		july4 := independenceDay(year)

		holidays := []string{
			june19.Format("2006-01-02"),
			july4.Format("2006-01-02"),
		}

		for _, month := range []time.Month{time.June, time.July, time.August} {
			p := types.UtilityVPPPeriod{
				TimePeriod: types.TimePeriod{
					Start: time.Date(year, month, 1, 0, 0, 0, 0, etLocation),
					End:   time.Date(year, month+1, 1, 0, 0, 0, 0, etLocation),
					Hours: []types.UtilityHourPeriod{
						{HourStart: 17, HourEnd: 20},
					},
					DaysOfTheWeek: []time.Weekday{
						time.Monday,
						time.Tuesday,
						time.Wednesday,
						time.Thursday,
						time.Friday,
					},
					SpecificDates:    holidays,
					SpecificDatesNot: true,
					LocationPtr:      etLocation,
				},
				ReserveSOC: 20,
			}
			periods = append(periods, p)
		}
	}

	return types.UtilityVPPInfo{
		Mandatory: periods,
	}, nil
}

// parseVPPHistory parses raw HTML from Eversource's VPP history page and extracts
// daily rates found across month tables. If targetStart or targetEnd are provided (non-zero),
// parsing will filter to dates in [targetStart, targetEnd] and short-circuit early once older dates are encountered.
func parseVPPHistory(ctx context.Context, r io.Reader, targetStart, targetEnd time.Time) ([]vppDailyRate, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var rates []vppDailyRate
	// Recursively traverse the DOM tree to locate all <table> elements.
	var parseTable func(*html.Node)
	parseTable = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "table" {
			ratesFromTable := parseSingleVPPTable(ctx, n, targetStart, targetEnd)
			rates = append(rates, ratesFromTable...)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			parseTable(c)
		}
	}
	parseTable(doc)
	return rates, nil
}

// monthOverlaps returns true if sampleMonth matches any month in the target range [targetStart, targetEnd].
func monthOverlaps(sampleMonth time.Month, targetStart, targetEnd time.Time) bool {
	if targetStart.IsZero() && targetEnd.IsZero() {
		return true
	}
	start := targetStart
	end := targetEnd
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}

	curr := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, etLocation)
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, etLocation)
	for !curr.After(last) {
		if curr.Month() == sampleMonth {
			return true
		}
		curr = curr.AddDate(0, 1, 0)
	}
	return false
}

// parseSingleVPPTable parses a single <table> node from the VPP history HTML.
// Each table on Eversource's VPP history page corresponds to a single calendar month
// (e.g. June) containing multi-year rate history (2026, 2025, 2024, ...).
func parseSingleVPPTable(ctx context.Context, tableNode *html.Node, targetStart, targetEnd time.Time) []vppDailyRate {
	// Locate <thead> and <tbody> sections within the table.
	var thead *html.Node
	var tbody *html.Node
	for c := tableNode.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			if c.Data == "thead" {
				thead = c
			} else if c.Data == "tbody" {
				tbody = c
			}
		}
	}
	if thead == nil || tbody == nil {
		return nil
	}

	// Extract header rows (tr elements in <thead>).
	// Row 0 defines top-level categories (e.g. Date, Last Resort, Residential).
	// Row 1 defines sub-categories (e.g. On-Peak, Off-Peak for each rate class).
	var trs []*html.Node
	for c := thead.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "tr" {
			trs = append(trs, c)
		}
	}
	if len(trs) < 2 {
		return nil
	}

	categoryRowCells := getHTMLCells(trs[0])
	subCategoryRowCells := getHTMLCells(trs[1])

	resOnPeakCol := -1
	resOffPeakCol := -1
	foundResidential := false

	// Track the current column offset by accounting for cell colspans.
	currCol := 0
	for _, cell := range categoryRowCells {
		text := strings.TrimSpace(getHTMLText(cell))
		colspan := getHTMLColspan(cell)

		// Look for the "Residential" header block.
		if strings.EqualFold(text, "residential") {
			foundResidential = true
			// Scan the sub-category row within the column range covered by this cell.
			for subCol := currCol; subCol < currCol+colspan && subCol < len(subCategoryRowCells); subCol++ {
				subText := strings.TrimSpace(getHTMLText(subCategoryRowCells[subCol]))
				if strings.EqualFold(subText, "on-peak") || strings.EqualFold(subText, "on peak") {
					resOnPeakCol = subCol
				} else if strings.EqualFold(subText, "off-peak") || strings.EqualFold(subText, "off peak") {
					resOffPeakCol = subCol
				}
			}
			break
		}
		currCol += colspan
	}

	if !foundResidential {
		log.Ctx(ctx).WarnContext(ctx, "failed to find residential category header in eversource vpp table")
		return nil
	}

	// Return early if the Residential On-Peak or Off-Peak column indices were not found.
	if resOnPeakCol < 0 || resOffPeakCol < 0 {
		log.Ctx(ctx).WarnContext(
			ctx,
			"failed to find on-peak or off-peak sub-header for residential in eversource vpp table",
			slog.Int("onPeakCol", resOnPeakCol),
			slog.Int("offPeakCol", resOffPeakCol),
		)
		return nil
	}

	var results []vppDailyRate
	var sampleDate time.Time
	var foundSampleDate bool

	// Sample the first valid date row to determine the table's calendar month.
	// Each table on Eversource's page represents a single month (e.g. June) spanning multiple years.
	// If the table's month does not fall within [targetStart, targetEnd], we can skip the entire table.
	for tr := tbody.FirstChild; tr != nil; tr = tr.NextSibling {
		if tr.Type != html.ElementNode || tr.Data != "tr" {
			continue
		}
		cells := getHTMLCells(tr)
		if len(cells) > 0 {
			dateStr := strings.TrimSpace(getHTMLText(cells[0]))
			if d, err := time.ParseInLocation("01/02/2006", dateStr, etLocation); err == nil {
				sampleDate = d
				foundSampleDate = true
				break
			}
		}
	}

	// Bail out early if the table's month does not overlap with targetStart..targetEnd.
	if foundSampleDate && !monthOverlaps(sampleDate.Month(), targetStart, targetEnd) {
		return nil
	}

	// Iterate through data rows in <tbody> to extract dates and prices.
	for tr := tbody.FirstChild; tr != nil; tr = tr.NextSibling {
		if tr.Type != html.ElementNode || tr.Data != "tr" {
			continue
		}
		cells := getHTMLCells(tr)
		if len(cells) == 0 {
			continue
		}

		// Column 0 contains the effective date string (MM/DD/YYYY).
		dateStr := strings.TrimSpace(getHTMLText(cells[0]))
		date, err := time.ParseInLocation("01/02/2006", dateStr, etLocation)
		if err != nil {
			log.Ctx(ctx).WarnContext(ctx, "failed to parse date from eversource vpp row", slog.String("dateStr", dateStr), slog.Any("error", err))
			continue
		}

		// Filter out dates outside [targetStart, targetEnd].
		if (!targetEnd.IsZero() && date.After(targetEnd)) || (!targetStart.IsZero() && date.Before(targetStart)) {
			// we skip if date is outside the target range
			// but we don't assume the table is sorted so we don't skip the rest of the table
			continue
		}

		if len(cells) <= resOnPeakCol || len(cells) <= resOffPeakCol {
			log.Ctx(ctx).WarnContext(
				ctx,
				"row matching target date has insufficient cells for residential on-peak/off-peak prices",
				slog.String("dateStr", dateStr),
				slog.Int("numCells", len(cells)),
				slog.Int("onPeakCol", resOnPeakCol),
				slog.Int("offPeakCol", resOffPeakCol),
			)
			continue
		}

		onPeakStr := strings.TrimSpace(getHTMLText(cells[resOnPeakCol]))
		offPeakStr := strings.TrimSpace(getHTMLText(cells[resOffPeakCol]))

		onPeakPrice, err1 := strconv.ParseFloat(onPeakStr, 64)
		offPeakPrice, err2 := strconv.ParseFloat(offPeakStr, 64)
		if err1 != nil || err2 != nil {
			log.Ctx(ctx).WarnContext(
				ctx,
				"failed to parse prices from eversource vpp row",
				slog.String("dateStr", dateStr),
				slog.String("onPeakStr", onPeakStr),
				slog.String("offPeakStr", offPeakStr),
				slog.Any("errOnPeak", err1),
				slog.Any("errOffPeak", err2),
			)
			continue
		}

		results = append(results, vppDailyRate{
			date:         date,
			onPeakPrice:  onPeakPrice,
			offPeakPrice: offPeakPrice,
		})
	}

	return results
}

// getHTMLCells returns all child <th> or <td> elements under a <tr> node.
func getHTMLCells(trNode *html.Node) []*html.Node {
	var cells []*html.Node
	for c := trNode.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "th" || c.Data == "td") {
			cells = append(cells, c)
		}
	}
	return cells
}

// getHTMLText recursively extracts and concatenates all text content inside an HTML node.
func getHTMLText(n *html.Node) string {
	var sb strings.Builder
	var f func(*html.Node)
	f = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return sb.String()
}

// getHTMLColspan parses the "colspan" attribute of an HTML cell, defaulting to 1 if unspecified.
func getHTMLColspan(n *html.Node) int {
	for _, attr := range n.Attr {
		if attr.Key == "colspan" {
			v, err := strconv.Atoi(attr.Val)
			if err == nil && v > 0 {
				return v
			}
		}
	}
	return 1
}

func dailyRateToHourlyPrices(rate vppDailyRate) []types.Price {
	prices := make([]types.Price, 24)
	year, month, day := rate.date.Year(), rate.date.Month(), rate.date.Day()
	weekday := rate.date.Weekday()
	isWeekday := weekday >= time.Monday && weekday <= time.Friday

	for h := 0; h < 24; h++ {
		tsStart := time.Date(year, month, day, h, 0, 0, 0, etLocation)
		tsEnd := tsStart.Add(time.Hour)

		isOnPeak := isWeekday && h >= eversourceVPPOnPeakStartHour && h < eversourceVPPOnPeakEndHour
		var priceVal float64
		var name string
		if isOnPeak {
			priceVal = rate.onPeakPrice
			name = "Eversource CT VPP On-Peak"
		} else {
			priceVal = rate.offPeakPrice
			name = "Eversource CT VPP Off-Peak"
		}

		prices[h] = types.Price{
			TSStart:       tsStart,
			TSEnd:         tsEnd,
			DollarsPerKWH: priceVal,
			PeriodName:    name,
		}
	}
	return prices
}

func (c *baseEversourceVPP) fetchHistoryAndCache(ctx context.Context) error {
	_, err, _ := c.sfGroup.Do("vppHistoryFetch", func() (interface{}, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", c.vppHistoryURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request for eversource vpp history: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch eversource vpp history: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("eversource vpp history returned status code %d", resp.StatusCode)
		}

		now := time.Now().In(etLocation)
		targetStart := truncateDay(now.AddDate(0, 0, -30))
		targetEnd := truncateDay(now.AddDate(0, 0, 2))

		rates, err := parseVPPHistory(ctx, resp.Body, targetStart, targetEnd)
		if err != nil {
			return nil, fmt.Errorf("failed to parse eversource vpp history: %w", err)
		}

		tomorrowDate := truncateDay(now).AddDate(0, 0, 1)
		for _, r := range rates {
			if truncateDay(r.date).Equal(tomorrowDate) {
				log.Ctx(ctx).DebugContext(
					ctx,
					"eversource vpp history contains rates for tomorrow",
					slog.Time("nowET", now),
					slog.Time("tomorrowDate", tomorrowDate),
				)
				break
			}
		}

		c.mu.Lock()
		toUpsert := make([]types.PriceState, 0, len(rates)*24)
		for _, r := range rates {
			hourly := dailyRateToHourlyPrices(r)
			dateStr := r.date.Format("2006-01-02")
			c.cachedPrices[dateStr] = hourly

			for _, p := range hourly {
				toUpsert = append(toUpsert, types.PriceState{
					Price:     p,
					Confirmed: true,
					TSUpdated: now,
				})
			}
		}
		c.mu.Unlock()

		if c.db != nil && len(toUpsert) > 0 {
			threeDaysAgo := truncateDay(now.AddDate(0, 0, -3))

			// Check if we already have data in the database from 3 days ago till now.
			existingRecent, err := c.db.GetUtilityPrices(ctx, "eversource_ct_vpp", threeDaysAgo, now)
			if err == nil && len(existingRecent) > 0 {
				// We already have recent data in DB; ignore historical rates older than 3 days ago.
				filteredUpsert := make([]types.PriceState, 0, len(toUpsert))
				for _, ps := range toUpsert {
					if !ps.Price.TSStart.Before(threeDaysAgo) {
						filteredUpsert = append(filteredUpsert, ps)
					}
				}
				toUpsert = filteredUpsert
			}
			// If len(existingRecent) == 0, we don't have data for recent days, so we upsert all 30 days.

			if len(toUpsert) > 0 {
				if err := c.db.UpsertUtilityPrices(ctx, "eversource_ct_vpp", toUpsert, 0); err != nil {
					log.Ctx(ctx).WarnContext(ctx, "failed to upsert eversource vpp prices to database", slog.Any("error", err))
				}
			}
		}
		return nil, nil
	})
	return err
}

func (c *baseEversourceVPP) getPricesForRange(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	startET := start.In(etLocation)
	endET := end.In(etLocation)
	firstDay := truncateDay(startET)
	lastDay := truncateDay(endET)

	// Step 1: Check if all days in [firstDay, lastDay] are already in memory cache.
	c.mu.Lock()
	allInCache := true
	var cachedPrices []types.Price
	currDay := firstDay
	for !currDay.After(lastDay) {
		dateStr := currDay.Format("2006-01-02")
		prices, ok := c.cachedPrices[dateStr]
		if !ok {
			allInCache = false
			break
		}
		for _, p := range prices {
			if !p.TSStart.Before(startET) && p.TSStart.Before(endET) {
				cachedPrices = append(cachedPrices, p)
			}
		}
		currDay = currDay.AddDate(0, 0, 1)
	}
	c.mu.Unlock()

	if allInCache {
		return cachedPrices, nil
	}

	// Step 2: Query database for the entire range in a single range query.
	if c.db != nil {
		dbStart := firstDay
		dbEnd := lastDay.AddDate(0, 0, 1)
		dbPrices, err := c.db.GetUtilityPrices(ctx, "eversource_ct_vpp", dbStart, dbEnd)
		if err == nil && len(dbPrices) > 0 {
			c.mu.Lock()
			byDate := make(map[string][]types.Price)
			for _, p := range dbPrices {
				dateStr := truncateDay(p.Price.TSStart.In(etLocation)).Format("2006-01-02")
				byDate[dateStr] = append(byDate[dateStr], p.Price)
			}
			for dStr, pList := range byDate {
				c.cachedPrices[dStr] = pList
			}
			c.mu.Unlock()

			// Check if EVERY day in [firstDay, lastDay] is present in the DB results.
			allDaysFound := true
			currDay := firstDay
			for !currDay.After(lastDay) {
				dateStr := currDay.Format("2006-01-02")
				if len(byDate[dateStr]) == 0 {
					allDaysFound = false
					break
				}
				currDay = currDay.AddDate(0, 0, 1)
			}

			if allDaysFound {
				var result []types.Price
				for _, p := range dbPrices {
					if !p.Price.TSStart.Before(startET) && p.Price.TSStart.Before(endET) {
						result = append(result, p.Price)
					}
				}
				return result, nil
			}
		}
	}

	log.Ctx(ctx).DebugContext(
		ctx,
		"fetching eversource vpp prices",
		slog.Time("start", start),
		slog.Time("end", end),
	)

	// Step 3: Fetch history from web if missing from cache and DB.
	if err := c.fetchHistoryAndCache(ctx); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	var result []types.Price
	currDay = firstDay
	for !currDay.After(lastDay) {
		dateStr := currDay.Format("2006-01-02")
		if prices, ok := c.cachedPrices[dateStr]; ok {
			for _, p := range prices {
				if !p.TSStart.Before(startET) && p.TSStart.Before(endET) {
					result = append(result, p)
				}
			}
		}
		currDay = currDay.AddDate(0, 0, 1)
	}
	return result, nil
}

func (c *baseEversourceVPP) getPricesForDate(ctx context.Context, date time.Time) ([]types.Price, error) {
	dayStart := truncateDay(date.In(etLocation))
	dayEnd := dayStart.AddDate(0, 0, 1)
	return c.getPricesForRange(ctx, dayStart, dayEnd)
}

func (c *baseEversourceVPP) GetCurrentPrice(ctx context.Context) (types.Price, error) {
	now := time.Now().In(etLocation)
	prices, err := c.getPricesForDate(ctx, now)
	if err != nil {
		return types.Price{}, err
	}
	for _, p := range prices {
		if p.Contains(now) {
			return p, nil
		}
	}
	return types.Price{}, fmt.Errorf("no current eversource vpp price found for %s", now.Format(time.RFC3339))
}

func (c *baseEversourceVPP) GetFuturePrices(ctx context.Context) ([]types.Price, error) {
	now := time.Now().In(etLocation)
	today := truncateDay(now)
	tomorrowEnd := today.AddDate(0, 0, 2)

	prices, err := c.getPricesForRange(ctx, today, tomorrowEnd)
	if err != nil {
		return nil, err
	}

	var futurePrices []types.Price
	for _, p := range prices {
		if !p.TSEnd.Before(now) {
			futurePrices = append(futurePrices, p)
		}
	}
	return futurePrices, nil
}

func (c *baseEversourceVPP) GetConfirmedPrices(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	return c.getPricesForRange(ctx, start, end)
}
