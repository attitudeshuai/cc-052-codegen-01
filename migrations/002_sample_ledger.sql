BEGIN;

-- 样品台账主表：一块地取的样，从取样一路跟到检测结论
CREATE TABLE IF NOT EXISTS sample (
    id              BIGSERIAL PRIMARY KEY,
    sample_no       VARCHAR(32) NOT NULL,
    plot_id         BIGINT NOT NULL REFERENCES plot(id),
    batch_id        BIGINT REFERENCES crop_batch(id),
    crop_id         VARCHAR(64) NOT NULL,
    sampler         VARCHAR(128) NOT NULL,          -- 取样人
    sampled_at      TIMESTAMPTZ NOT NULL,           -- 取样时间
    quantity        INT NOT NULL DEFAULT 1 CHECK (quantity > 0), -- 取样份数
    unit            VARCHAR(16) NOT NULL DEFAULT '份',
    status          VARCHAR(16) NOT NULL DEFAULT 'sampled'
                    CHECK (status IN ('sampled','in_lab','concluded','void')),
    void_reason     VARCHAR(255),
    void_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 样品编号不许重号（作废也占用编号，永不复用）
CREATE UNIQUE INDEX IF NOT EXISTS idx_sample_no ON sample(sample_no);
CREATE INDEX IF NOT EXISTS idx_sample_plot ON sample(plot_id);
CREATE INDEX IF NOT EXISTS idx_sample_batch ON sample(batch_id);
CREATE INDEX IF NOT EXISTS idx_sample_status ON sample(status);
CREATE INDEX IF NOT EXISTS idx_sample_sampled_at ON sample(sampled_at);

-- 编号日序号：YP + yyyyMMdd + 4 位流水，按天取号
CREATE TABLE IF NOT EXISTS sample_seq (
    seq_date    DATE PRIMARY KEY,
    last_seq    INT NOT NULL DEFAULT 0
);

-- 样品交接台账：交出去 / 收回来，每一段都记数量与双方
CREATE TABLE IF NOT EXISTS sample_custody (
    id              BIGSERIAL PRIMARY KEY,
    sample_id       BIGINT NOT NULL REFERENCES sample(id),
    direction       VARCHAR(8) NOT NULL CHECK (direction IN ('send','return')),
    quantity        INT NOT NULL CHECK (quantity > 0),
    handler_from    VARCHAR(128) NOT NULL,
    handler_to      VARCHAR(128) NOT NULL,
    transferred_at  TIMESTAMPTZ NOT NULL,
    note            VARCHAR(255),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_custody_sample ON sample_custody(sample_id);
CREATE INDEX IF NOT EXISTS idx_custody_time ON sample_custody(transferred_at);

-- 留样台账：结论没回来之前，留样放在哪个柜子、哪天到期
CREATE TABLE IF NOT EXISTS sample_reserve (
    id              BIGSERIAL PRIMARY KEY,
    sample_id       BIGINT NOT NULL UNIQUE REFERENCES sample(id),
    quantity        INT NOT NULL CHECK (quantity > 0),
    cabinet         VARCHAR(64) NOT NULL,           -- 留样柜编号
    stored_at       TIMESTAMPTZ NOT NULL,
    expire_at       TIMESTAMPTZ NOT NULL,           -- 留样到期时间
    disposed_at     TIMESTAMPTZ,                    -- 实际处置时间
    dispose_method  VARCHAR(32) CHECK (dispose_method IN ('destroy','retain','return')),
    note            VARCHAR(255),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reserve_expire ON sample_reserve(expire_at);
CREATE INDEX IF NOT EXISTS idx_reserve_disposed ON sample_reserve(disposed_at);

-- 检测结论：同一样品可出现多份结论（复检/异常留痕），active 决定是否计入统计
CREATE TABLE IF NOT EXISTS sample_conclusion (
    id              BIGSERIAL PRIMARY KEY,
    sample_id       BIGINT NOT NULL REFERENCES sample(id),
    lab             VARCHAR(255) NOT NULL,
    result          VARCHAR(8) NOT NULL CHECK (result IN ('pass','fail')),
    concluded_at    TIMESTAMPTZ NOT NULL,
    report_no       VARCHAR(64),
    report_url      TEXT,
    items           JSONB DEFAULT '[]',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,  -- FALSE 即判为挂错/作废的结论，剔除统计
    invalidate_reason VARCHAR(255),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 同一份样品的有效结论只准一份：第二份当场指认为重复结论
CREATE UNIQUE INDEX IF NOT EXISTS idx_conclusion_active_one
    ON sample_conclusion(sample_id) WHERE is_active;
-- 检测报告号不许重（挂错样品时同号会立刻暴露）
CREATE UNIQUE INDEX IF NOT EXISTS idx_conclusion_report_no
    ON sample_conclusion(report_no) WHERE report_no IS NOT NULL AND is_active;
CREATE INDEX IF NOT EXISTS idx_conclusion_sample ON sample_conclusion(sample_id);

COMMIT;
