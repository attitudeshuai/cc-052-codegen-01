BEGIN;

-- 样品台账：取样登记（地块、作物、取样人、取样时间）
CREATE TABLE IF NOT EXISTS sample (
    id BIGSERIAL PRIMARY KEY,
    sample_no VARCHAR(64) NOT NULL,
    plot_id BIGINT NOT NULL REFERENCES plot(id),
    crop VARCHAR(128) NOT NULL,
    sampler VARCHAR(128) NOT NULL,
    sampled_at TIMESTAMPTZ NOT NULL,
    quantity INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    status VARCHAR(16) NOT NULL DEFAULT 'registered' CHECK (status IN ('registered','sent','returned','concluded','void')),
    cabinet VARCHAR(64) NOT NULL DEFAULT '',
    retain_until DATE,
    disposed_at TIMESTAMPTZ,
    disposal_method VARCHAR(32) NOT NULL DEFAULT '',
    disposed_by VARCHAR(128) NOT NULL DEFAULT '',
    void_reason VARCHAR(255) NOT NULL DEFAULT '',
    note VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 样品编号不许重号
CREATE UNIQUE INDEX IF NOT EXISTS idx_sample_sample_no ON sample(sample_no);
CREATE INDEX IF NOT EXISTS idx_sample_plot ON sample(plot_id);
CREATE INDEX IF NOT EXISTS idx_sample_status ON sample(status);
CREATE INDEX IF NOT EXISTS idx_sample_retain ON sample(retain_until) WHERE disposed_at IS NULL;

-- 送检/收回交接单（direction: out=交出去, in=收回来）
CREATE TABLE IF NOT EXISTS sample_transfer (
    id BIGSERIAL PRIMARY KEY,
    direction VARCHAR(8) NOT NULL CHECK (direction IN ('out','in')),
    out_transfer_id BIGINT REFERENCES sample_transfer(id),
    lab VARCHAR(255) NOT NULL DEFAULT '',
    handler VARCHAR(128) NOT NULL,
    happened_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sample_transfer_out ON sample_transfer(out_transfer_id);

-- 交接明细
CREATE TABLE IF NOT EXISTS sample_transfer_item (
    id BIGSERIAL PRIMARY KEY,
    transfer_id BIGINT NOT NULL REFERENCES sample_transfer(id),
    sample_id BIGINT NOT NULL REFERENCES sample(id),
    quantity INT NOT NULL CHECK (quantity > 0)
);

CREATE INDEX IF NOT EXISTS idx_transfer_item_transfer ON sample_transfer_item(transfer_id);
CREATE INDEX IF NOT EXISTS idx_transfer_item_sample ON sample_transfer_item(sample_id);

-- 检测结论：同一份样品只允许一份结论（唯一索引兜底）
CREATE TABLE IF NOT EXISTS sample_conclusion (
    id BIGSERIAL PRIMARY KEY,
    sample_id BIGINT NOT NULL REFERENCES sample(id),
    lab VARCHAR(255) NOT NULL DEFAULT '',
    result VARCHAR(8) NOT NULL CHECK (result IN ('pass','fail')),
    concluded_at TIMESTAMPTZ NOT NULL,
    report_url TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sample_conclusion_sample ON sample_conclusion(sample_id);

COMMIT;
