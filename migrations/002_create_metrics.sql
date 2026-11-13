-- ==============================================================================
-- Migration 002: Create TPS Metrics Table & Indexes
-- ==============================================================================

IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'tps_metrics' AND schema_id = SCHEMA_ID('dbo'))
BEGIN
    CREATE TABLE dbo.tps_metrics (
        application_name VARCHAR(50) NOT NULL,
        source_name VARCHAR(50) NOT NULL,
        metric_time DATETIME2(0) NOT NULL,
        total_transactions INT NOT NULL CONSTRAINT DF_tps_metrics_total DEFAULT 0,
        success_transactions INT NOT NULL CONSTRAINT DF_tps_metrics_success DEFAULT 0,
        failed_transactions INT NOT NULL CONSTRAINT DF_tps_metrics_failed DEFAULT 0,
        is_complete BIT NOT NULL CONSTRAINT DF_tps_metrics_complete DEFAULT 1,
        collected_at DATETIME2(3) NOT NULL CONSTRAINT DF_tps_metrics_collected DEFAULT SYSUTCDATETIME(),
        CONSTRAINT PK_tps_metrics PRIMARY KEY CLUSTERED (application_name, source_name, metric_time)
    );
END;

IF NOT EXISTS (SELECT * FROM sys.indexes WHERE name = 'IX_tps_metrics_time' AND object_id = OBJECT_ID('dbo.tps_metrics'))
BEGIN
    CREATE NONCLUSTERED INDEX IX_tps_metrics_time 
    ON dbo.tps_metrics (source_name, metric_time) 
    INCLUDE (total_transactions, success_transactions, failed_transactions);
END;

