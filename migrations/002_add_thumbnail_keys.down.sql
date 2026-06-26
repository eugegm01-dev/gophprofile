DROP INDEX IF EXISTS idx_avatars_thumbnails;
ALTER TABLE avatars DROP COLUMN IF EXISTS thumbnail_s3_keys;
