package repository

import (
	"cc-052/internal/model"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// IsUniqueViolation 判断是否唯一约束冲突
func IsUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

type SampleRepo struct {
	db *sqlx.DB
}

func NewSampleRepo(db *sqlx.DB) *SampleRepo {
	return &SampleRepo{db: db}
}

const sampleColumns = `id, sample_no, plot_id, batch_id, crop_id, sampler, sampled_at,
	quantity, unit, status, void_reason, void_at, created_at`

// cstZone 业务按中国作业日发号（UTC+8 固定偏移，不依赖系统 tzdata）
var cstZone = time.FixedZone("CST", 8*60*60)

// NextSampleNo 按天发号：YP + yyyyMMdd + 4 位流水。行级 UPSERT 保证并发不重号。
func (r *SampleRepo) NextSampleNo(tx *sqlx.Tx, sampledAt time.Time) (string, error) {
	day := sampledAt.In(cstZone)
	date := day.Format("2006-01-02")
	var seq int
	query := `INSERT INTO sample_seq (seq_date, last_seq) VALUES ($1::date, 1)
	          ON CONFLICT (seq_date) DO UPDATE SET last_seq = sample_seq.last_seq + 1
	          RETURNING last_seq`
	if err := tx.Get(&seq, query, date); err != nil {
		return "", err
	}
	return fmt.Sprintf("YP%s%04d", day.Format("20060102"), seq), nil
}

// Create 落台账；sampleNo 为空时在事务内自动发号。
func (r *SampleRepo) Create(s *model.Sample) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if s.SampleNo == "" {
		no, err := r.NextSampleNo(tx, s.SampledAt)
		if err != nil {
			return err
		}
		s.SampleNo = no
	}

	query := `INSERT INTO sample (sample_no, plot_id, batch_id, crop_id, sampler, sampled_at, quantity, unit, status)
	          VALUES (:sample_no, :plot_id, :batch_id, :crop_id, :sampler, :sampled_at, :quantity, :unit, :status)
	          RETURNING id, created_at`
	stmt, err := tx.PrepareNamed(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	if err := stmt.QueryRowx(s).Scan(&s.ID, &s.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SampleRepo) GetByID(id int64) (*model.Sample, error) {
	var s model.Sample
	query := `SELECT ` + sampleColumns + ` FROM sample WHERE id = $1`
	if err := r.db.Get(&s, query, id); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SampleRepo) GetBySampleNo(no string) (*model.Sample, error) {
	var s model.Sample
	query := `SELECT ` + sampleColumns + ` FROM sample WHERE sample_no = $1`
	if err := r.db.Get(&s, query, no); err != nil {
		return nil, err
	}
	return &s, nil
}

// SampleFilter 台账查询条件
type SampleFilter struct {
	PlotID   int64
	BatchID  int64
	Status   string
	Sampler  string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

func (r *SampleRepo) List(f SampleFilter) ([]model.Sample, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	idx := 1
	add := func(cond string, val interface{}) {
		where = append(where, fmt.Sprintf(cond, idx))
		args = append(args, val)
		idx++
	}
	if f.PlotID != 0 {
		add("plot_id = $%d", f.PlotID)
	}
	if f.BatchID != 0 {
		add("batch_id = $%d", f.BatchID)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Sampler != "" {
		add("sampler = $%d", f.Sampler)
	}
	if f.From != nil {
		add("sampled_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("sampled_at <= $%d", *f.To)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	countQuery := "SELECT COUNT(*) FROM sample WHERE " + whereSQL
	if err := r.db.Get(&total, countQuery, args...); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT ` + sampleColumns + ` FROM sample WHERE ` + whereSQL +
		fmt.Sprintf(` ORDER BY sampled_at DESC, id DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	args = append(args, limit, f.Offset)

	var samples []model.Sample
	if err := r.db.Select(&samples, query, args...); err != nil {
		return nil, 0, err
	}
	return samples, total, nil
}

func (r *SampleRepo) UpdateStatus(id int64, status model.SampleStatus) error {
	_, err := r.db.Exec(`UPDATE sample SET status = $1 WHERE id = $2`, status, id)
	return err
}

// Void 作废：作废样品不再进入统计，编号保留不复用
func (r *SampleRepo) Void(id int64, reason string, at time.Time) error {
	res, err := r.db.Exec(
		`UPDATE sample SET status = 'void', void_reason = $1, void_at = $2 WHERE id = $3 AND status <> 'void'`,
		reason, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
