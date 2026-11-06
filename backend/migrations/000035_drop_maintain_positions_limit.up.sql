-- MaintainPositions no longer exists: the 35=AL position write path is removed.
DELETE FROM rate_limit_policies WHERE operation = 'MaintainPositions';
