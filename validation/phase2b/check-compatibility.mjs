// Check the locale keys and migration SQL against the Phase 2A baseline.
import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
const flatten=(o,p='')=>Object.entries(o).flatMap(([k,v])=>typeof v==='object'?flatten(v,p+k+'.'):[p+k]);
const phase1Commit='4accd35e6b7a801274e034adc103f59df37e686c';
const phase2aKeys=['tasks.verificationPending','tasks.status.RECOVERY_REQUIRED',...['FILE_NOT_FOUND','WITNESS_MISMATCH','WITNESS_UNAVAILABLE','WITNESS_RECORD_FAILED','RECOVERY_REQUIRED','STORAGE_BUSY','STORAGE_READ_ONLY','STORAGE_FULL','STORAGE_CORRUPT','STORAGE_IO','STORAGE_CONSTRAINT','STORAGE_UNAVAILABLE','STORAGE_SCHEMA_VERSION','PROFILE_IN_USE'].map(x=>'errors.'+x),'audit.TASK_RECOVERY_REQUIRED','audit.TASK_RECOVERED'];
const result={};
for(const locale of ['en-US','zh-CN']){
 const old=JSON.parse(execFileSync('git',['show',`${phase1Commit}:frontend/src/locales/${locale}.json`],{encoding:'utf8'}));
 const current=new Set(flatten(JSON.parse(await fs.readFile(`frontend/src/locales/${locale}.json`,'utf8'))));
 const baseline=new Set([...flatten(old),...phase2aKeys]);
 for(const key of baseline)assert.ok(current.has(key),`${locale} removed ${key}`);
 result[locale]={phase2aKeysPreserved:baseline.size,currentKeys:current.size};
}
const current=(await fs.readFile('internal/app/store.go','utf8')).replaceAll('\r','');
const migrationHashes={
 schemaV1:'cfac238c679b4032d29cd49c2e9d705ef0294b59c2fd2cd75b5b8f4a511a894b',
 schemaV2:'102d4246cd8e9c9171c652175f086e78ffa615666966aecee9b8b75c053e65a0',
};
for(const [name,expected] of Object.entries(migrationHashes)){
 const re=new RegExp('const '+name+' = `([\\s\\S]*?)`');
 const match=current.match(re);
 assert.ok(match,name);
 assert.equal(createHash('sha256').update(match[1]).digest('hex'),expected,name+' changed');
 result[name]='UNCHANGED';
}
console.log(JSON.stringify(result));
