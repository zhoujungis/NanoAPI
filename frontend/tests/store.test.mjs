import test from 'node:test';
import assert from 'node:assert/strict';

import { moveItem } from '../dist/js/store.js';

function sampleTree() {
  return [
    {
      id: 'a',
      type: 'folder',
      name: 'A',
      items: [
        { id: 'a1', type: 'request', name: 'A1', request: { id: 'a1' } },
        { id: 'a2', type: 'request', name: 'A2', request: { id: 'a2' } },
      ],
    },
    { id: 'b', type: 'folder', name: 'B', items: [] },
    { id: 'c', type: 'request', name: 'C', request: { id: 'c' } },
  ];
}

const ids = (items) => (items || []).map((item) => item.id);

test('moves a request into a folder', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a1', 'b', 'inside'), true);
  assert.deepEqual(ids(tree[0].items), ['a2']);
  assert.deepEqual(ids(tree[1].items), ['a1']);
});

test('reorders folders before a sibling', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'b', 'a', 'before'), true);
  assert.deepEqual(ids(tree), ['b', 'a', 'c']);
});

test('reorders a request after its sibling', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a1', 'a2', 'after'), true);
  assert.deepEqual(ids(tree[0].items), ['a2', 'a1']);
});

test('rejects moving a folder inside its descendant', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a', 'a1', 'inside'), false);
  assert.deepEqual(ids(tree), ['a', 'b', 'c']);
  assert.deepEqual(ids(tree[0].items), ['a1', 'a2']);
});

test('rejects moving a folder before its descendant', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a', 'a1', 'before'), false);
  assert.deepEqual(ids(tree), ['a', 'b', 'c']);
});

test('rejects dropping inside a request', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a1', 'c', 'inside'), false);
  assert.deepEqual(ids(tree[0].items), ['a1', 'a2']);
});

test('moves a nested request to the root', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a1', '', 'root'), true);
  assert.deepEqual(ids(tree), ['a', 'b', 'c', 'a1']);
  assert.deepEqual(ids(tree[0].items), ['a2']);
});

test('moves a child before its parent folder', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a1', 'a', 'before'), true);
  assert.deepEqual(ids(tree), ['a1', 'a', 'b', 'c']);
  assert.deepEqual(ids(tree[1].items), ['a2']);
});

test('rejects invalid drop positions without changing the tree', () => {
  const tree = sampleTree();
  assert.equal(moveItem(tree, 'a1', 'b', 'sideways'), false);
  assert.deepEqual(ids(tree), ['a', 'b', 'c']);
  assert.deepEqual(ids(tree[0].items), ['a1', 'a2']);
});
