package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/jameshartig/autoenergy/pkg/types"
	"github.com/levenlabs/go-lflag"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FirestoreProvider implements the Provider interface using Google Cloud Firestore.
// It persists settings, prices, and actions to Firestore collections.
type FirestoreProvider struct {
	client    *firestore.Client
	projectID string
	database  string
}

// configuredFirestore sets up the Firestore provider.
// It registers flags for configuration.
func configuredFirestore() *FirestoreProvider {
	projectID := lflag.String("firestore-project-id", "", "Google Cloud Project ID for Firestore")
	database := lflag.String("firestore-database", "", "Google Cloud Firestore Database")
	emulator := lflag.String("firestore-emulator", "", "Use Firestore emulator")

	f := &FirestoreProvider{}

	lflag.Do(func() {
		f.projectID = *projectID
		f.database = *database

		// set this because that's how firestore client expects it
		if *emulator != "" {
			os.Setenv("FIRESTORE_EMULATOR_HOST", *emulator)
		}
	})

	return f
}

// Validate checks if the provider is properly configured.
func (f *FirestoreProvider) Validate() error {
	// Project ID verification could be here, but we allow empty if inferred.
	return nil
}

// Init initializes the Firestore client.
// This must be called before using the provider methods.
func (f *FirestoreProvider) Init(ctx context.Context) error {
	projectID := f.projectID
	if projectID == "" {
		projectID = firestore.DetectProjectID
	}
	database := f.database
	if database == "" {
		database = firestore.DefaultDatabaseID
	}
	client, err := firestore.NewClientWithDatabase(ctx, projectID, database)
	if err != nil {
		return fmt.Errorf("failed to create firestore client (project=%s, database=%s): %w", projectID, database, err)
	}
	f.client = client
	return nil
}

// Close closes the Firestore client connection.
func (f *FirestoreProvider) Close() error {
	if f.client != nil {
		return f.client.Close()
	}
	return nil
}

// GetSettings retrieves the dynamic configuration from the "config/settings" document.
func (f *FirestoreProvider) GetSettings(ctx context.Context) (types.Settings, error) {
	doc, err := f.client.Collection("config").Doc("settings").Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// Return default settings if not found
			return types.Settings{}, nil
		}
		return types.Settings{}, fmt.Errorf("failed to fetch settings doc: %w", err)
	}

	val, err := doc.DataAt("json")
	if err != nil {
		return types.Settings{}, fmt.Errorf("settings document missing 'json' field: %w", err)
	}

	jsonStr, ok := val.(string)
	if !ok {
		return types.Settings{}, fmt.Errorf("settings 'json' field is not a string")
	}

	var s types.Settings
	if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
		return types.Settings{}, fmt.Errorf("failed to unmarshal settings json: %w", err)
	}
	return s, nil
}

// SetSettings saves the dynamic configuration to the "config/settings" document.
// It stores the settings as a JSON string for portability.
func (f *FirestoreProvider) SetSettings(ctx context.Context, settings types.Settings) error {
	jsonBytes, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	_, err = f.client.Collection("config").Doc("settings").Set(ctx, map[string]interface{}{
		"json": string(jsonBytes),
	})
	if err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}
	return nil
}

// UpsertPrice adds or updates a price record in the "utility_prices" collection.
// The document ID is the RFC3339 timestamp of TSStart for efficient range queries.
func (f *FirestoreProvider) UpsertPrice(ctx context.Context, price types.Price) error {
	jsonBytes, err := json.Marshal(price)
	if err != nil {
		return fmt.Errorf("failed to marshal price: %w", err)
	}

	docID := price.TSStart.UTC().Format(time.RFC3339)
	_, err = f.client.Collection("utility_prices").Doc(docID).Set(ctx, map[string]interface{}{
		"json": string(jsonBytes),
	})
	if err != nil {
		return fmt.Errorf("failed to upsert price: %w", err)
	}
	return nil
}

// InsertAction adds a new action record to the "actions" collection as a JSON blob.
// The document ID is the RFC3339 timestamp for efficient range queries.
func (f *FirestoreProvider) InsertAction(ctx context.Context, action types.Action) error {
	jsonBytes, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("failed to marshal action: %w", err)
	}

	// Use RFC3339 as document ID for lexicographic ordering and efficient range queries
	docID := action.Timestamp.UTC().Format(time.RFC3339)
	_, err = f.client.Collection("actions").Doc(docID).Set(ctx, map[string]interface{}{
		"json": string(jsonBytes),
	})
	if err != nil {
		return fmt.Errorf("failed to insert action: %w", err)
	}
	return nil
}

// GetPriceHistory retrieves price records within the specified time range.
// Uses document ID range queries for efficient filtering.
func (f *FirestoreProvider) GetPriceHistory(ctx context.Context, start, end time.Time) ([]types.Price, error) {
	startDocID := start.UTC().Format(time.RFC3339)
	endDocID := end.UTC().Format(time.RFC3339)

	coll := f.client.Collection("utility_prices")
	iter := coll.
		Where(firestore.DocumentID, ">=", coll.Doc(startDocID)).
		Where(firestore.DocumentID, "<", coll.Doc(endDocID)).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var prices []types.Price
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating prices: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return nil, fmt.Errorf("price document %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("price document %s 'json' field is not string", doc.Ref.ID)
		}

		var p types.Price
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal price (id=%s): %w", doc.Ref.ID, err)
		}
		prices = append(prices, p)
	}
	return prices, nil
}

// GetActionHistory retrieves action records within the specified time range.
// Uses document ID range queries for efficient filtering without reading all documents.
func (f *FirestoreProvider) GetActionHistory(ctx context.Context, start, end time.Time) ([]types.Action, error) {
	startDocID := start.UTC().Format(time.RFC3339)
	endDocID := end.UTC().Format(time.RFC3339)

	coll := f.client.Collection("actions")
	iter := coll.
		Where(firestore.DocumentID, ">=", coll.Doc(startDocID)).
		Where(firestore.DocumentID, "<", coll.Doc(endDocID)).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var actions []types.Action
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating actions: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return nil, fmt.Errorf("action document %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("action document %s 'json' field is not string", doc.Ref.ID)
		}

		var a types.Action
		if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
			return nil, fmt.Errorf("failed to unmarshal action (id=%s): %w", doc.Ref.ID, err)
		}
		actions = append(actions, a)
	}
	return actions, nil
}

// UpsertEnergyHistory adds or updates an energy history record in the "energy_hourly" collection.
// The document ID is the RFC3339 timestamp of TSHourStart for consistent formatting.
func (f *FirestoreProvider) UpsertEnergyHistory(ctx context.Context, stats types.EnergyStats) error {
	if stats.TSHourStart.IsZero() {
		return fmt.Errorf("energy stats missing tsHourStart")
	}
	jsonBytes, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal energy stats: %w", err)
	}

	docID := stats.TSHourStart.UTC().Format(time.RFC3339)
	_, err = f.client.Collection("energy_hourly").Doc(docID).Set(ctx, map[string]interface{}{
		"json":      string(jsonBytes),
		"timestamp": stats.TSHourStart,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert energy history: %w", err)
	}
	return nil
}

// GetEnergyHistory retrieves energy history records within the specified time range.
func (f *FirestoreProvider) GetEnergyHistory(ctx context.Context, start, end time.Time) ([]types.EnergyStats, error) {
	startDocID := start.Truncate(time.Hour).UTC().Format(time.RFC3339)
	endDocID := end.Truncate(time.Hour).UTC().Format(time.RFC3339)

	coll := f.client.Collection("energy_hourly")
	iter := coll.
		Where(firestore.DocumentID, ">=", coll.Doc(startDocID)).
		Where(firestore.DocumentID, "<", coll.Doc(endDocID)).
		OrderBy(firestore.DocumentID, firestore.Asc).
		Documents(ctx)
	defer iter.Stop()

	var allStats []types.EnergyStats
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error iterating hourly energy history: %w", err)
		}

		val, err := doc.DataAt("json")
		if err != nil {
			return nil, fmt.Errorf("energy stats doc %s missing 'json' field: %w", doc.Ref.ID, err)
		}

		jsonStr, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("energy stats doc %s 'json' field is not string", doc.Ref.ID)
		}

		var s types.EnergyStats
		if err := json.Unmarshal([]byte(jsonStr), &s); err != nil {
			return nil, fmt.Errorf("failed to unmarshal energy stats (id=%s): %w", doc.Ref.ID, err)
		}
		allStats = append(allStats, s)
	}
	return allStats, nil
}

// GetLatestEnergyHistoryTime retrieves the timestamp of the last stored energy history record.
func (f *FirestoreProvider) GetLatestEnergyHistoryTime(ctx context.Context) (time.Time, error) {
	iter := f.client.Collection("energy_hourly").
		OrderBy("timestamp", firestore.Desc).
		Limit(1).
		Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get latest energy history doc: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, doc.Ref.ID)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid energy history doc id %s: %w", doc.Ref.ID, err)
	}
	return ts, nil
}
