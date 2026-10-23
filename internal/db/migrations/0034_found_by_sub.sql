-- When each subscription last brought in a new file (the Downloads page marks the ones that have found nothing for
-- months): the newest downloaded file_sources row per subscription.
CREATE INDEX file_sources_sub_found ON file_sources (subscription_id, first_at) WHERE subscription_id IS NOT NULL AND downloaded = 1;
