-- Username login. The admin account is created from ADMIN_USERNAME at boot.
CREATE PROPERTY User.username IF NOT EXISTS STRING;
CREATE INDEX IF NOT EXISTS ON User (username) UNIQUE NULL_STRATEGY SKIP;
