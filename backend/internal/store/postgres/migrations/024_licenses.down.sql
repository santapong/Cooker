-- 024_licenses.down.sql
--
-- Reverse 024: drop the licenses table (its CHECK constraint goes with
-- it). The instance returns to the unlicensed (free/explorer) baseline.

DROP TABLE IF EXISTS licenses;
