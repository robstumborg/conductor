const IDLE_DELAY_MS = 350

const pendingIdleTimers = new Map()
const sessionIdleSequence = new Map()
const sessionErrorSuppressionAt = new Map()
const sessionLastBusyAt = new Map()

const recentPermissionNotifications = new Map()
const recentQuestionNotifications = new Map()

function firstString(...values) {
  for (const value of values) {
    if (typeof value === "string") {
      const trimmed = value.trim()
      if (trimmed !== "") {
        return trimmed
      }
    }
  }
  return ""
}

function truncate(value, max = 240) {
  if (value.length <= max) {
    return value
  }
  return value.slice(0, max - 3).trimEnd() + "..."
}

function getSessionID(event) {
  return firstString(event?.properties?.sessionID)
}

function clearPendingIdleTimer(sessionID) {
  const timer = pendingIdleTimers.get(sessionID)
  if (!timer) {
    return
  }
  clearTimeout(timer)
  pendingIdleTimers.delete(sessionID)
}

function bumpSessionIdleSequence(sessionID) {
  const nextSequence = (sessionIdleSequence.get(sessionID) ?? 0) + 1
  sessionIdleSequence.set(sessionID, nextSequence)
  return nextSequence
}

function hasCurrentSessionIdleSequence(sessionID, sequence) {
  return sessionIdleSequence.get(sessionID) === sequence
}

function markSessionBusy(sessionID) {
  if (!sessionID) {
    return
  }
  sessionLastBusyAt.set(sessionID, Date.now())
  sessionErrorSuppressionAt.delete(sessionID)
  bumpSessionIdleSequence(sessionID)
  clearPendingIdleTimer(sessionID)
}

function markSessionError(sessionID) {
  if (!sessionID) {
    return
  }
  sessionErrorSuppressionAt.set(sessionID, Date.now())
  bumpSessionIdleSequence(sessionID)
  clearPendingIdleTimer(sessionID)
}

function shouldSuppressSessionIdle(sessionID, consume = true) {
  const errorAt = sessionErrorSuppressionAt.get(sessionID)
  if (errorAt === undefined) {
    return false
  }
  const busyAt = sessionLastBusyAt.get(sessionID)
  if (typeof busyAt === "number" && busyAt > errorAt) {
    sessionErrorSuppressionAt.delete(sessionID)
    return false
  }
  if (consume) {
    sessionErrorSuppressionAt.delete(sessionID)
  }
  return true
}

function formatQuestionMessage(args) {
  const questions = Array.isArray(args?.questions) ? args.questions : []
  const prompts = questions
    .map((question) => firstString(question?.question, question?.header))
    .filter(Boolean)
  const text = firstString(prompts.join(" / "), args?.question, args?.message, args?.prompt)
  return truncate(text)
}

function formatPermissionMessage(event) {
  const props = event?.properties ?? {}
  const tool = firstString(props.tool)
  const command = firstString(props.command)
  const message = firstString(props.message, props.prompt)
  return truncate(firstString(message, command, tool, "Agent requested permission"))
}

function formatErrorMessage(event) {
  const error = event?.properties?.error ?? {}
  return truncate(firstString(error.message, error.name, event?.properties?.message, "OpenCode session failed"))
}

function shouldNotifyPermission(sessionID) {
  const key = firstString(sessionID, "global")
  const now = Date.now()
  const lastAt = recentPermissionNotifications.get(key)
  if (typeof lastAt === "number" && now - lastAt < 1000) {
    return false
  }

  recentPermissionNotifications.set(key, now)
  for (const [knownKey, timestamp] of recentPermissionNotifications) {
    if (now - timestamp >= 10000) {
      recentPermissionNotifications.delete(knownKey)
    }
  }

  return true
}

function shouldNotifyQuestion(signature) {
  const key = firstString(signature)
  if (!key) {
    return true
  }

  const now = Date.now()
  const lastAt = recentQuestionNotifications.get(key)
  if (typeof lastAt === "number" && now - lastAt < 1500) {
    return false
  }

  recentQuestionNotifications.set(key, now)

  for (const [knownKey, timestamp] of recentQuestionNotifications) {
    if (now - timestamp >= 10000) {
      recentQuestionNotifications.delete(knownKey)
    }
  }

  return true
}

async function getSessionInfo(client, sessionID) {
  if (!sessionID) {
    return { title: "", isChild: false }
  }
  try {
    const response = await client.session.get({ path: { id: sessionID } })
    return {
      title: firstString(response?.data?.title),
      isChild: !!response?.data?.parentID,
    }
  } catch {
    return { title: "", isChild: false }
  }
}

async function runConductNotify(client, cwd, eventName, details) {
  const cmd = ["conduct", "notify", "--event", eventName]
  if (details.title) {
    cmd.push("--title", details.title)
  }
  if (details.message) {
    cmd.push("--message", details.message)
  }
  if (details.branch) {
    cmd.push("--branch", details.branch)
  }
  if (details.model) {
    cmd.push("--model", details.model)
  }

  try {
    const proc = Bun.spawn({
      cmd,
      cwd,
      stdout: "ignore",
      stderr: "pipe",
    })
    const exitCode = await proc.exited
    if (exitCode === 0) {
      return
    }
    const stderr = await new Response(proc.stderr).text()
    await client.app.log({
      body: {
        service: "conductor-notify",
        level: "warn",
        message: "conduct notify exited non-zero",
        extra: { eventName, exitCode, stderr: stderr.trim() },
      },
    })
  } catch (error) {
    await client.app.log({
      body: {
        service: "conductor-notify",
        level: "warn",
        message: "failed to invoke conduct notify",
        extra: { eventName, error: String(error) },
      },
    })
  }
}

async function notifyQuestion(client, cwd, sessionID, args, signature) {
  if (!shouldNotifyQuestion(signature)) {
    return
  }

  const session = await getSessionInfo(client, sessionID)
  await runConductNotify(client, cwd, "question", {
    title: firstString(session.title, "Agent question"),
    message: formatQuestionMessage(args),
  })
}

function scheduleIdle(client, cwd, event, sessionID) {
  clearPendingIdleTimer(sessionID)
  const sequence = bumpSessionIdleSequence(sessionID)
  const timer = setTimeout(() => {
    pendingIdleTimers.delete(sessionID)
    void processIdle(client, cwd, event, sessionID, sequence)
  }, IDLE_DELAY_MS)
  pendingIdleTimers.set(sessionID, timer)
}

async function processIdle(client, cwd, event, sessionID, sequence) {
  if (!hasCurrentSessionIdleSequence(sessionID, sequence)) {
    return
  }
  if (shouldSuppressSessionIdle(sessionID)) {
    return
  }

  const session = await getSessionInfo(client, sessionID)
  if (session.isChild) {
    return
  }
  if (!hasCurrentSessionIdleSequence(sessionID, sequence)) {
    return
  }
  if (shouldSuppressSessionIdle(sessionID)) {
    return
  }

  await runConductNotify(client, cwd, "idle", {
    title: firstString(session.title, "OpenCode session idle"),
    message: "Session is idle",
  })
}

export const ConductorNotifyPlugin = async ({ client, directory, worktree }) => {
  const cwd = worktree || directory

  return {
    event: async ({ event }) => {
      if (event.type === "question.asked") {
        const props = event.properties ?? {}
        await notifyQuestion(
          client,
          cwd,
          props.sessionID,
          { questions: props.questions },
          firstString(props.id, props.sessionID, formatQuestionMessage({ questions: props.questions })),
        )
        return
      }

      if (event.type === "permission.asked") {
        const sessionID = getSessionID(event)
        if (!shouldNotifyPermission(sessionID)) {
          return
        }
        const session = await getSessionInfo(client, sessionID)
        await runConductNotify(client, cwd, "permission", {
          title: firstString(session.title, "Permission requested"),
          message: formatPermissionMessage(event),
        })
        return
      }

      if (event.type === "session.status" && event.properties?.status?.type === "busy") {
        markSessionBusy(getSessionID(event))
        return
      }

      if (event.type === "session.error") {
        const sessionID = getSessionID(event)
        markSessionError(sessionID)
        const session = await getSessionInfo(client, sessionID)
        await runConductNotify(client, cwd, "error", {
          title: firstString(session.title, "OpenCode session error"),
          message: formatErrorMessage(event),
        })
        return
      }

      if (event.type === "session.idle") {
        const sessionID = getSessionID(event)
        if (!sessionID) {
          await runConductNotify(client, cwd, "idle", {
            title: "OpenCode session idle",
            message: "Session is idle",
          })
          return
        }
        scheduleIdle(client, cwd, event, sessionID)
      }
    },
    "permission.ask": async (input) => {
      if (!shouldNotifyPermission(input?.sessionID)) {
        return
      }
      await runConductNotify(client, cwd, "permission", {
        title: "Permission requested",
        message: "Agent requested permission",
      })
    },
    "tool.execute.before": async (input, output) => {
      if (input.tool !== "question") {
        return
      }
      await notifyQuestion(
        client,
        cwd,
        input.sessionID,
        output?.args,
        firstString(input.callID, input.sessionID, formatQuestionMessage(output?.args)),
      )
    },
  }
}

export default ConductorNotifyPlugin
