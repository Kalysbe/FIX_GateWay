CREATE TABLE rate_limit_policies (
 scope TEXT NOT NULL CHECK (scope IN ('global','broker','admin')),
 subject TEXT NOT NULL,
 operation TEXT NOT NULL,
 rate INTEGER NOT NULL CHECK (rate BETWEEN 1 AND 1000000),
 burst INTEGER NOT NULL CHECK (burst BETWEEN 1 AND 1000000),
 concurrent INTEGER NOT NULL CHECK (concurrent BETWEEN 1 AND 1000000),
 enabled BOOLEAN NOT NULL DEFAULT true,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(scope,subject,operation)
);
-- Existing broker rates remain the defaults; these policies add explicit
-- global protection and independently protect authenticated admin commands.
INSERT INTO rate_limit_policies(scope,subject,operation,rate,burst,concurrent) VALUES
 ('global','*','Trading',50000,1000,500),
 ('global','*','Queries',10000,1000,500),
 ('global','*','System',1000,200,128),
 ('admin','*','System',10,10,128),
 ('admin','*','Connect',2,4,4);
