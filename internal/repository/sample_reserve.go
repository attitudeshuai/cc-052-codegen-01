package repository

import (
	"cc-052/internal/model"
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type ReserveRepo struct {
	db *sqlx.DB
}

func NewReserveRepo(db *sqlx.DB) *ReserveRepo {
	return &ReserveRepo{db: db}
}

const reserveColumns = `id, sample_id, quantity, cabinet, stored_at, expire_at, disposed_at, dispose_method, note, created_at`

func (r *ReserveRepo) Create(x *model.SampleReserve) error {
	query := `INSERT INTO sample_reserve (sample_id, quantity, cabinet, stored_at, expire_at, note)
	          VALUES (:sample_id, :quantity, :cabinet, :stored_at, :expire_at, :note)
	          RETURNING id, created_at`
	stmt, err := r.db.PrepareNamed(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	return stmt.QueryRowx(x).Scan(&x.ID, &x.CreatedAt)
}

func (r *ReserveRepo) GetBySample(sampleID int64) (*model.SampleReserve, error) {
	var x model.SampleReserve
	query := `SELECT ` + reserveColumns + ` FROM sample_reserve WHERE sample_id = $1`
	if err := r.db.Get(&x, query, sampleID); err != nil {
		return nil, err
	}
	return &x, nil
}

// Dispose 记录到期处置
func (r *ReserveRepo) Dispose(sampleID int64, method model.DisposeMethod, at time.Time, note *string) error {
	res, err := r.db.Exec(
		`UPDATE sample_reserve SET disposed_at = $1, dispose_method = $2, note = COALESCE($3, note)
		 WHERE sample_id = $4 AND disposed_at IS NULL`,
		at, method, note, sampleID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Retain 批准续存：只顺延到期日，disposed_at 保持 NULL，
// 这样样品在新的到期日之后会再次进入到期清单。
func (r *ReserveRepo) Retain(sampleID int64, newExpireAt time.Time, note *string) error {
	res, err := r.db.Exec(
		`UPDATE sample_reserve SET expire_at = $1, note = COALESCE($2, note)
		 WHERE sample_id = $3 AND disposed_at IS NULL`,
		newExpireAt, note, sampleID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Expired 到期留样单独列出：到期未处置的留样，含样品编号/柜子/数量/到期时间/样品状态。
// at 一般传当前时间。
func (r *ReserveRepo) Expired(at time.Time) ([]model.ExpiredReserve, error) {
	query := `
		SELECT r.sample_id, s.sample_no, r.cabinet, r.quantity, r.expire_at, s.status AS sample_status
		FROM sample_reserve r
		JOIN sample s ON s.id = r.sample_id
		WHERE r.disposed_at IS NULL
		  AND r.expire_at <= $1
		  AND s.status <> 'void'
		ORDER BY r.expire_at ASC, s.sample_no ASC`
	var rows []model.ExpiredReserve
	if err := r.db.Select(&rows, query, at); err != nil {
		return nil, err
	}
	return rows, nil
}

// ExpiredCount 统计用
func (r *ReserveRepo) ExpiredCount(at time.Time) (int64, error) {
	var n int64
	query := `SELECT COUNT(*) FROM sample_reserve r
	          JOIN sample s ON s.id = r.sample_id
	          WHERE r.disposed_at IS NULL AND r.expire_at <= $1 AND s.status <> 'void'`
	if err := r.db.Get(&n, query, at); err != nil {
		return 0, err
	}
	return n, nil
}
