#!/usr/bin/env python3
"""Integration checks against a local Nakama using disposable test accounts."""
import base64
import concurrent.futures
import json
import os
import urllib.error
import urllib.parse
import urllib.request
import uuid

BASE = os.environ.get('RIFTBOUND_TEST_URL', 'http://127.0.0.1:7350')
SERVER_KEY = os.environ.get('RIFTBOUND_TEST_SERVER_KEY', 'riftbound-debug-key')
ADMIN_KEY = os.environ.get('RIFT_NAKAMA_ADMIN_HTTP_KEY', 'riftbound-debug-http')
checks = 0

def request(path, body=None, token=None, basic=None, method=None):
    headers = {'Content-Type': 'application/json'}
    if token: headers['Authorization'] = 'Bearer ' + token
    if basic: headers['Authorization'] = 'Basic ' + base64.b64encode((basic + ':').encode()).decode()
    req = urllib.request.Request(BASE + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)

def rpc(name, body, token=None, admin=False):
    suffix = '?' + urllib.parse.urlencode({'http_key': ADMIN_KEY}) if admin else ''
    result = request('/v2/rpc/' + name + suffix, json.dumps(body), token)
    return json.loads(result['payload'])

def check(value, description):
    global checks
    checks += 1
    if not value: raise AssertionError(description)

def reject(fn, description):
    try: fn()
    except urllib.error.HTTPError as error:
        check(400 <= error.code < 500, description + ' failed with server error ' + str(error.code))
        return
    raise AssertionError(description + ' unexpectedly accepted')

def authenticate(device):
    return request('/v2/account/authenticate/device?create=true', {'id': device}, basic=SERVER_KEY)['token']

def main():
    device = 'rift-storage-test-' + uuid.uuid4().hex
    a, b = authenticate(device), authenticate(device + '-b')
    state = rpc('player_bootstrap', {}, a)
    second = rpc('player_bootstrap', {}, b)
    uid = state['user_id']; config = state['config']; version = state['config_version']
    check(uid != second['user_id'], 'distinct accounts')
    check(config['economy']['all_unlocked'], 'temporary free access stays enabled')
    check(len(state['profile']['owned_characters']) == 40, 'only starter collection permanently owned')
    check(state['wallet']['coins'] == 0, 'wallet initializes to zero')
    cached = rpc('player_bootstrap', {'cached_config_version': version}, a)
    check('config' not in cached, 'unchanged catalog cache avoids full download')
    check(cached['profile_version'] == state['profile_version'], 'bootstrap does not overwrite player data')
    owned = set(state['profile']['owned_characters'])
    candidates = [c['character_id'] for c in config['catalog']['characters'] if c['enabled'] and c['character_id'] not in owned]
    character = candidates[0]; price = config['economy']['prices'][character]
    purchase = {'character_id': character, 'request_id': uuid.uuid4().hex, 'config_version': version, 'price': 0, 'user_id': second['user_id']}
    reject(lambda: rpc('character_purchase', purchase, a), 'insufficient funds')
    unchanged = rpc('player_bootstrap', {'cached_config_version': version}, a)
    check(character not in unchanged['profile']['owned_characters'] and unchanged['wallet']['coins'] == 0, 'failed purchase rolls back')
    reject(lambda: rpc('admin_player_grant', {'user_id': uid, 'coins': 1000, 'request_id': uuid.uuid4().hex}, a), 'player cannot grant currency')
    reject(lambda: rpc('admin_config_get', {}, a), 'player cannot access config administration')
    grant = {'user_id': uid, 'coins': price, 'request_id': uuid.uuid4().hex}
    rpc('admin_player_grant', grant, admin=True); rpc('admin_player_grant', grant, admin=True)
    funded = rpc('player_bootstrap', {'cached_config_version': version}, a)
    check(funded['wallet']['coins'] == price, 'grant receipt prevents duplicate credit')
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(lambda _: rpc('character_purchase', purchase, a), range(2)))
    after = rpc('player_bootstrap', {'cached_config_version': version}, a)
    check(after['wallet']['coins'] == 0, 'retry/concurrent purchase charges once at server price')
    check(after['profile']['owned_characters'].count(character) == 1, 'ownership granted once')
    check(character not in rpc('player_bootstrap', {}, b)['profile']['owned_characters'], 'other account unaffected')
    wrong = dict(purchase, character_id=candidates[1])
    reject(lambda: rpc('character_purchase', wrong, a), 'request ID cannot change item')
    deck = dict(after['profile']['decks'][0]); deck['name'] = 'Saved on server'
    save = {'decks': [deck], 'selected_deck_id': deck['id'], 'profile_version': after['profile_version'], 'config_version': version}
    saved = rpc('player_decks_save', save, a)
    check(saved['profile']['decks'][0]['name'] == 'Saved on server', 'server deck save')
    reject(lambda: rpc('player_decks_save', save, a), 'stale profile version rejected')
    storage = request('/v2/storage', {'object_ids': [{'collection': 'riftbound_player', 'key': 'collection_decks', 'user_id': uid}]}, a, method='POST')
    check(len(storage['objects']) == 1, 'owner can read player storage')
    obj = storage['objects'][0]
    check(obj['permission_read'] == 1 and obj.get('permission_write', 0) == 0, 'owner-read/server-write permissions')
    reject(lambda: request('/v2/storage', {'objects': [{'collection': 'riftbound_player', 'key': 'collection_decks', 'value': '{}', 'version': obj['version']}]}, a, method='PUT'), 'client cannot forge ownership')
    foreign = request('/v2/storage', {'object_ids': [{'collection': 'riftbound_player', 'key': 'collection_decks', 'user_id': uid}]}, b, method='POST')
    check(not foreign.get('objects'), 'other users cannot read private decks')
    # Reauthenticate the same persisted identity: saved decks/collection/wallet survive a new login.
    a = authenticate(device)
    reloaded = rpc('player_bootstrap', {}, a)
    check(reloaded['user_id'] == uid and reloaded['profile']['decks'][0]['name'] == 'Saved on server', 'cross-login persisted decks')
    check(character in reloaded['profile']['owned_characters'] and reloaded['wallet']['coins'] == 0, 'cross-login persisted ownership/wallet')
    published = rpc('admin_config_get', {}, admin=True)
    staged = rpc('admin_config_publish', {'storage_version': published['storage_version'], 'economy': published['config']['economy']}, admin=True)
    check(staged['restart_required'] and staged['config_version'] == version, 'validated same-config publish, immutable active revision')
    print('PLAYER STORAGE INTEGRATION PASS (%d checks): separate accounts, cache version, purchase atomicity, receipts, wallet, private storage, deck CAS, relogin, administration' % checks)

if __name__ == '__main__': main()
