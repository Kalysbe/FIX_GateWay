package quikexport

import (
	"context"
	"encoding/json"
)

// pageRows runs a page query whose rows are (row_to_json, count(*) OVER ()):
// the window count is taken before LIMIT, so every row carries the number of
// rows of the whole selection. A page past the end has no row to carry it, and
// the query is then read again from the start for the total alone.
func (s *Store) pageRows(ctx context.Context, query string, args func(limit, offset int) []any, limit, offset int) ([]json.RawMessage, int, error) {
	raws, total, err := s.scanPage(ctx, query, args(limit, offset))
	if err == nil && len(raws) == 0 && offset > 0 {
		_, total, err = s.scanPage(ctx, query, args(1, 0))
	}
	return raws, total, err
}

func (s *Store) scanPage(ctx context.Context, query string, args []any) ([]json.RawMessage, int, error) {
	out := []json.RawMessage{}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return out, 0, err
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw, &total); err != nil {
			return out, 0, err
		}
		out = append(out, raw)
	}
	return out, int(total), rows.Err()
}

// pageState is what every page reports besides its rows.
func pageState(offset, count, total int) (hasMore bool, nextOffset int) {
	return offset+count < total, offset + count
}
