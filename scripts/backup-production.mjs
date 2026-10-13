import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

// A consistent checkpoint includes both PostgreSQL and FIX sequence/inbox state.
// Run this from an operator-owned scheduler; it briefly stops the trading process.
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const test=process.argv.includes('--test-fixture');
const option=process.argv.indexOf('--env-file');
assert.ok(option>=0&&process.argv[option+1],'--env-file required');
const project=test?'fix-gateway-production-verification':'fix-gateway-production';
const compose=['compose','--env-file',path.resolve(process.argv[option+1]),'-p',project,'-f',path.join(root,'deploy/docker-compose.production.yml')];
if(test)compose.push('-f',path.join(root,'output/hardening-2026-10-12/production-fixture.override.yml'));
const command=args=>execFileSync(process.env.DOCKER_BIN||'docker',[...compose,...args],{cwd:root,encoding:'utf8',timeout:300000,stdio:['ignore','pipe','inherit']});
const config=JSON.parse(command(['config','--format','json']));
assert.equal(config.name,project);assert.equal(config.services.gateway.environment.APP_ENV,'production');
assert.ok(config.services.backup.volumes.some(v=>v.target==='/state'&&v.read_only),'read-only FIX state mount required');
const stamp=new Date().toISOString().replace(/[-:]/g,'').replace(/\.\d+Z/,'Z');
assert.match(stamp,/^\d{8}T\d{6}Z$/);
const prefix=`/backups/checkpoint-${stamp}`;
let stopped=false;
try{
 stopped=true;command(['stop','-t','40','gateway']);
 command(['run','--rm','--no-deps','-T','--entrypoint','/bin/sh','backup','-ec',`set -eu; set -o pipefail; umask 077;
 pg_dump --dbname="$(cat "$DATABASE_URL_FILE")" --format=custom --no-owner | age -r "$BACKUP_AGE_RECIPIENT" -o '${prefix}.dump.age.pending';
 tar -C /state -cf - . | age -r "$BACKUP_AGE_RECIPIENT" -o '${prefix}.fix.tar.age.pending';
 mv '${prefix}.dump.age.pending' '${prefix}.dump.age';
 mv '${prefix}.fix.tar.age.pending' '${prefix}.fix.tar.age';
 sha256sum '${prefix}.dump.age' '${prefix}.fix.tar.age' > '${prefix}.sha256.pending';
 mv '${prefix}.sha256.pending' '${prefix}.sha256';
 date -u +%s > /backups/last-checkpoint;
 find /backups -maxdepth 1 -type f -name 'checkpoint-*.sha256' -mtime +7 | while IFS= read -r manifest; do
  old="\${manifest%.sha256}";
  case "$old" in /backups/checkpoint-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z) rm -f "$old.dump.age" "$old.fix.tar.age" "$manifest" ;; esac;
 done;`]);
 console.log(`Consistent encrypted database/FIX checkpoint completed: ${stamp}`);
}finally{
 if(stopped)command(['start','gateway']);
}
