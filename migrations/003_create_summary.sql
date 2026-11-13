-- ==============================================================================
-- Migration 003: Create TPS Summary Table
-- ==============================================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'tps_summary' AND schema_id = SCHEMA_ID('dbo'))
BEGIN
    CREATE TABLE dbo.tps_summary (
        application_name VARCHAR(50) NOT NULL,
        source_name VARCHAR(50) NOT NULL,
        period_type VARCHAR(20) NOT NULL, -- 'daily', 'weekly', 'monthly'
        period_start DATETIME2(0) NOT NULL,
        period_end DATETIME2(0) NOT NULL,
        min_tps INT NOT NULL,
        max_tps INT NOT NULL,
        avg_tps DECIMAL(10, 4) NOT NULL,
        total_transactions BIGINT NOT NULL,
        calculated_at DATETIME2(3) NOT NULL CONSTRAINT DF_tps_summary_calc DEFAULT SYSUTCDATETIME(),
        CONSTRAINT PK_tps_summary PRIMARY KEY CLUSTERED (application_name, source_name, period_type, period_start)
    );
END;

