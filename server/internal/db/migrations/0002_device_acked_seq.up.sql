-- How far a device has acknowledged the stream.
--
-- Needed to close the delivery timeline: without it the server records that
-- it *sent* an event but never learns whether the device *received* it, and
-- "sent without delivered" is precisely the reliability signal we want. The
-- client acknowledges a seq; everything up to it becomes delivered.
ALTER TABLE devices ADD COLUMN acked_seq INTEGER NOT NULL DEFAULT 0;
