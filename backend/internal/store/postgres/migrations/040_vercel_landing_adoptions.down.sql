-- An old worker must never receive future update jobs. Do not silently erase
-- authority or receipts while downgrading; qualify their disposition first.
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM vercel_landing_adoptions) THEN
  RAISE EXCEPTION 'landing adoption history requires an explicit retention decision before downgrade';
 END IF;
END $$;
DROP TABLE vercel_landing_adoptions;
