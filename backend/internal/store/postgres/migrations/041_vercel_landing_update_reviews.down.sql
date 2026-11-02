-- Retain immutable review history until an operator explicitly qualifies its
-- disposition. This migration never drops a populated receipt table silently.
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM vercel_landing_update_reviews) THEN
  RAISE EXCEPTION 'landing update review history requires an explicit retention decision before downgrade';
 END IF;
END $$;
DROP TABLE vercel_landing_update_reviews;
