#!/usr/bin/env python3
"""Trusted Nakama administration over the runtime HTTP key, never a player token."""
import argparse
import json
import os
import urllib.parse
import urllib.request

def rpc(base, key, name, body):
    url = base.rstrip('/') + '/v2/rpc/' + name + '?' + urllib.parse.urlencode({'http_key': key})
    request = urllib.request.Request(url, data=json.dumps(json.dumps(body, ensure_ascii=False), ensure_ascii=False).encode(), headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=20) as response:
        envelope = json.load(response)
    return json.loads(envelope['payload'])

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', default='http://127.0.0.1:7350')
    sub = parser.add_subparsers(dest='command', required=True)
    get = sub.add_parser('config-get'); get.add_argument('output')
    publish = sub.add_parser('config-publish'); publish.add_argument('input')
    grant = sub.add_parser('grant'); grant.add_argument('user_id'); grant.add_argument('--coins', type=int, default=0); grant.add_argument('--character', default=''); grant.add_argument('--request-id', required=True)
    args = parser.parse_args()
    key = os.environ.get('RIFT_NAKAMA_ADMIN_HTTP_KEY')
    if not key:
        parser.error('Set RIFT_NAKAMA_ADMIN_HTTP_KEY to the server runtime HTTP key.')
    if args.command == 'config-get':
        result = rpc(args.url, key, 'admin_config_get', {})
        with open(args.output, 'w') as stream:
            json.dump(result, stream, indent=2, ensure_ascii=False)
        print('Configuration saved to ' + args.output)
    elif args.command == 'config-publish':
        with open(args.input) as stream:
            body = json.load(stream)
        current = rpc(args.url, key, 'admin_config_get', {})
        if current['storage_version'] != body['storage_version']:
            raise SystemExit('Configuration changed. Export it again before publishing.')
        wanted = body['config']; existing = current['config']
        old_chars = existing['catalog']['characters']; new_chars = wanted['catalog']['characters']
        if wanted['schema_version'] != existing['schema_version'] or wanted['catalog']['group_names'] != existing['catalog']['group_names'] or len(new_chars) < len(old_chars):
            raise SystemExit('Schema and existing character/group identities must remain stable.')
        patches = {}
        for old, new in zip(old_chars, new_chars):
            if any(old[field] != new[field] for field in ('kind', 'character_id', 'group')):
                raise SystemExit('Existing character identities must remain stable.')
            patch = {field: value for field, value in new.items() if old.get(field) != value}
            if patch: patches[old['character_id']] = patch
        patch_body = {'storage_version': body['storage_version'], 'character_patches': patches, 'new_characters': new_chars[len(old_chars):]}
        if wanted['economy'] != existing['economy']: patch_body['economy'] = wanted['economy']
        if len(json.dumps(patch_body, ensure_ascii=False).encode()) > 128 * 1024:
            raise SystemExit('This change is too large. Publish a smaller set of changes.')
        print(json.dumps(rpc(args.url, key, 'admin_config_publish', patch_body), indent=2))
    else:
        result = rpc(args.url, key, 'admin_player_grant', {'user_id': args.user_id, 'coins': args.coins, 'character_id': args.character, 'request_id': args.request_id})
        print(json.dumps({'user_id': result['user_id'], 'wallet': result['wallet'], 'owned_characters': result['profile']['owned_characters']}, indent=2))
if __name__ == '__main__':
    main()
