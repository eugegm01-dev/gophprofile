ALTER TABLE avatars 
ADD COLUMN IF NOT EXISTS thumbnail_s3_keys JSONB DEFAULT '{}';

-- Индекс для быстрого поиска аватарок с миниатюрами
CREATE INDEX IF NOT EXISTS idx_avatars_thumbnails ON avatars USING GIN (thumbnail_s3_keys);
