import test from 'node:test'
import assert from 'node:assert/strict'
import { __test } from '../plugin/index.mjs'

const { findApprovalCandidate, failureBlock, eventPatch } = __test

test('picks exact approval by tool and callId', () => {
  const events = [
    { type: 'approval/asked', data: { id: 'old', toolName: 'bash', callId: 'call-old' } },
    { type: 'approval/decided', data: { id: 'old', outcome: 'allowed-once' } },
    { type: 'approval/asked', data: { id: 'now', toolName: 'bash', callId: 'call-new' } },
  ]
  assert.equal(findApprovalCandidate(events, { toolName: 'bash', callId: 'call-new' }).id, 'now')
})

test('falls back to newest unresolved ask when callId is absent', () => {
  const events = [
    { type: 'approval/asked', data: { id: 'a', toolName: 'bash' } },
    { type: 'approval/asked', data: { id: 'b', toolName: 'read' } },
  ]
  assert.equal(findApprovalCandidate(events, { toolName: 'write' }).id, 'b')
})

test('ignores decided asks', () => {
  const events = [
    { type: 'approval/asked', data: { id: 'a', toolName: 'bash' } },
    { type: 'approval/decided', data: { id: 'a', outcome: 'rejected' } },
  ]
  assert.equal(findApprovalCandidate(events, { toolName: 'bash' }), null)
})

test('maps quota failures to top-up title', () => {
  const block = failureBlock({ kind: 'error', error: { code: 'ACCOUNT_QUOTA', message: 'Insufficient balance' } })
  assert.equal(block.type, 'error')
  assert.match(block.title, /额度不足/)
})

test('maps approval asked event to compact patch', () => {
  const patch = eventPatch({ type: 'approval/asked', data: { id: 'x', toolName: 'bash', reason: 'need access' } })
  assert.equal(patch.approval.id, 'x')
  assert.equal(patch.state, 'waiting')
})

const { findQuestionCallId, normalizeQuestionAnswers } = __test

test('prefers the live wait callId for a question', () => {
  assert.equal(findQuestionCallId([], { wait: { callId: 'call-wait' } }), 'call-wait')
})

test('finds the question tool call by its question ids', () => {
  const events = [
    { type: 'tool/call', data: { callId: 'call-a', name: 'ask_user_question', arguments: JSON.stringify({ questions: [{ id: 'other' }] }) } },
    { type: 'tool/call', data: { callId: 'call-b', name: 'ask_user_question', arguments: JSON.stringify({ questions: [{ id: 'q1' }, { id: 'q2' }] }) } },
  ]
  assert.equal(findQuestionCallId(events, { questions: [{ id: 'q1' }, { id: 'q2' }] }), 'call-b')
})

test('normalizes a complete answer batch', () => {
  const entry = { questions: [{ id: 'q1' }, { id: 'q2' }] }
  const answers = normalizeQuestionAnswers(entry, [
    { id: 'q2', selected: ['B'], custom: '补充' },
    { id: 'q1', selected: ['A'] },
  ])
  assert.deepEqual(answers, [
    { id: 'q1', selected: ['A'] },
    { id: 'q2', selected: ['B'], custom: '补充' },
  ])
})

test('rejects an incomplete or duplicate answer batch', () => {
  const entry = { questions: [{ id: 'q1' }, { id: 'q2' }] }
  assert.equal(normalizeQuestionAnswers(entry, [{ id: 'q1', selected: [] }]), null)
  assert.equal(normalizeQuestionAnswers(entry, [{ id: 'q1' }, { id: 'q1' }]), null)
})
