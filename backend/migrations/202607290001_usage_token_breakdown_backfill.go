package migrations

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationNoTxContext(upUsageTokenBreakdownBackfill, nil)
}

func upUsageTokenBreakdownBackfill(ctx context.Context, db *sql.DB) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	updates, err := usageTokenBreakdownBackfillUpdates(ctx, tx)
	if err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `
			UPDATE usage_records
			SET input_tokens = ?, output_tokens = ?
			WHERE id = ?
		`, update.inputTokens, update.outputTokens, update.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type usageTokenBackfillUpdate struct {
	id           int64
	inputTokens  int64
	outputTokens int64
}

func usageTokenBreakdownBackfillUpdates(ctx context.Context, tx *sql.Tx) ([]usageTokenBackfillUpdate, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, input_tokens, output_tokens, raw_json
		FROM usage_records
		WHERE (input_tokens = 0 OR output_tokens = 0)
		  AND raw_json LIKE '%"token_breakdown"%'
		  AND raw_json LIKE '%"tokens"%'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	updates := []usageTokenBackfillUpdate{}
	for rows.Next() {
		var id, inputTokens, outputTokens int64
		var rawJSON string
		if err := rows.Scan(&id, &inputTokens, &outputTokens, &rawJSON); err != nil {
			return nil, err
		}
		canonicalInput, canonicalOutput, ok := parseCanonicalUsageTokenBreakdown(rawJSON)
		if !ok {
			continue
		}
		nextInput, nextOutput := inputTokens, outputTokens
		if inputTokens == 0 && canonicalInput > 0 {
			nextInput = canonicalInput
		}
		if outputTokens == 0 && canonicalOutput > 0 {
			nextOutput = canonicalOutput
		}
		if nextInput == inputTokens && nextOutput == outputTokens {
			continue
		}
		updates = append(updates, usageTokenBackfillUpdate{
			id:           id,
			inputTokens:  nextInput,
			outputTokens: nextOutput,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return updates, nil
}

type canonicalUsageTokens struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	TotalTokens  *int64 `json:"total_tokens"`
}

type canonicalUsageTokenBreakdown struct {
	Input struct {
		TotalTokens *int64 `json:"total_tokens"`
	} `json:"input"`
	Output struct {
		TotalTokens *int64 `json:"total_tokens"`
	} `json:"output"`
	TotalTokens *int64 `json:"total_tokens"`
}

func parseCanonicalUsageTokenBreakdown(rawJSON string) (int64, int64, bool) {
	var payload struct {
		Tokens         *canonicalUsageTokens         `json:"tokens"`
		TokenBreakdown *canonicalUsageTokenBreakdown `json:"token_breakdown"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil || payload.Tokens == nil || payload.TokenBreakdown == nil {
		return 0, 0, false
	}
	tokens := payload.Tokens
	breakdown := payload.TokenBreakdown
	if tokens.InputTokens == nil || tokens.OutputTokens == nil || tokens.TotalTokens == nil ||
		breakdown.Input.TotalTokens == nil || breakdown.Output.TotalTokens == nil || breakdown.TotalTokens == nil {
		return 0, 0, false
	}
	if *tokens.InputTokens < 0 || *tokens.OutputTokens < 0 || *tokens.TotalTokens < 0 ||
		*breakdown.Input.TotalTokens < 0 || *breakdown.Output.TotalTokens < 0 || *breakdown.TotalTokens < 0 {
		return 0, 0, false
	}
	if *tokens.InputTokens != *breakdown.Input.TotalTokens ||
		*tokens.OutputTokens != *breakdown.Output.TotalTokens ||
		*tokens.TotalTokens != *breakdown.TotalTokens {
		return 0, 0, false
	}
	if *tokens.TotalTokens < *tokens.InputTokens || *tokens.TotalTokens-*tokens.InputTokens != *tokens.OutputTokens {
		return 0, 0, false
	}
	return *tokens.InputTokens, *tokens.OutputTokens, true
}
