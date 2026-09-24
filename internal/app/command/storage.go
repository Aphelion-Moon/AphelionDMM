package command

import "github.com/rs/zerolog/log"

// NullSpaceStackId is for a stack which won't hold any command and will be always empty.
const NullSpaceStackId = "__NULL_SPACE__"

// Storage is used to store application command and handle undo/redo stuff.
type Storage struct {
	currentStackId string
	commandStacks  map[string]*commandStack
}

func NewStorage() *Storage {
	s := &Storage{commandStacks: make(map[string]*commandStack)}
	s.SetStack(NullSpaceStackId)
	return s
}

func (s *Storage) Free() {
	// APHELION EDIT ADDITION START - HISTORY LIFETIME
	for _, stack := range s.commandStacks {
		stack.clear()
	}
	s.currentStackId = ""
	// APHELION EDIT ADDITION END
	s.commandStacks = make(map[string]*commandStack, len(s.commandStacks))
	s.SetStack(NullSpaceStackId)
	log.Print("storage free")
}

func (s *Storage) SetStack(id string) {
	if s.currentStackId == id {
		return
	}

	log.Print("changing stack to:", id)

	s.currentStackId = id
	if _, ok := s.commandStacks[id]; !ok {
		s.commandStacks[id] = &commandStack{id: id}
		log.Print("created stack:", id)
	}
}

func (s *Storage) DisposeStack(id string) {
	if id == NullSpaceStackId {
		log.Print("skip disposing for:", id)
		return
	}

	log.Print("disposing stack:", id)
	// APHELION EDIT ADDITION START - HISTORY LIFETIME
	if stack := s.commandStacks[id]; stack != nil {
		stack.clear()
	}
	// APHELION EDIT ADDITION END
	delete(s.commandStacks, id)
	if s.currentStackId == id {
		s.SetStack(NullSpaceStackId)
	}
}

func (s *Storage) Push(command Command) {
	if s.currentStackId == NullSpaceStackId {
		log.Print("skip pushing for:", s.currentStackId)
		return
	}

	if stack, ok := s.commandStacks[s.currentStackId]; ok {
		/* APHELION EDIT REMOVAL START - HISTORY LIFETIME
		logStackAction(stack, "push command: "+command.name)
		stack.undo = append(stack.undo, command)
		stack.redo = stack.redo[:0]
		stack.balance++
		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION START - HISTORY LIFETIME
		stack.push(command)
		// APHELION EDIT ADDITION END
	} else {
		logNoStackAvailable("push command")
	}
}

// APHELION EDIT ADDITION START - HISTORY LIFETIME
// Target binds commands to one map's stack lifetime. Like Storage, it is owned
// by the UI thread. Switching tabs does not change the target; disposing a stack
// invalidates it even when a new map later uses the same stack ID.
type Target struct {
	storage *Storage
	stack   *commandStack
}

// Bind obtains a history target without changing the active undo/redo stack.
func (s *Storage) Bind(id string) Target {
	if s == nil || id == "" || id == NullSpaceStackId {
		return Target{}
	}
	if s.commandStacks[id] == nil {
		s.commandStacks[id] = &commandStack{id: id}
	}
	return Target{storage: s, stack: s.commandStacks[id]}
}

// APHELION EDIT ADDITION START - SAVE_AS_HISTORY
// CanRebindStack reports whether the source stack can change identity without
// replacing an already-owned destination stack. Busy stacks cannot be moved
// because async completions are scoped to their original identity.
func (s *Storage) CanRebindStack(fromID, toID string) bool {
	if s == nil || fromID == "" || toID == "" || fromID == NullSpaceStackId || toID == NullSpaceStackId {
		return false
	}
	stack := s.commandStacks[fromID]
	if stack == nil || stack.busy {
		return false
	}
	return fromID == toID || s.commandStacks[toID] == nil
}

// RebindStack moves one map's existing undo history to its Save As identity.
// Bound targets keep the same stack pointer and remain valid after the move.
func (s *Storage) RebindStack(fromID, toID string) bool {
	if !s.CanRebindStack(fromID, toID) {
		return false
	}
	if fromID == toID {
		return true
	}
	stack := s.commandStacks[fromID]
	delete(s.commandStacks, fromID)
	stack.id = toID
	s.commandStacks[toID] = stack
	if s.currentStackId == fromID {
		s.currentStackId = toID
	}
	return true
}

// APHELION EDIT ADDITION END

func (target Target) Valid() bool {
	return target.storage != nil && target.stack != nil && target.storage.commandStacks[target.stack.id] == target.stack
}

// Push returns false after disposal and never recreates a discarded stack.
func (target Target) Push(command Command) bool {
	if !target.Valid() {
		return false
	}
	target.stack.push(command)
	return true
}

func (stack *commandStack) push(command Command) {
	if stack.busy {
		logStackAction(stack, "queue command: "+command.name)
		stack.queued = append(stack.queued, command)
		return
	}
	logStackAction(stack, "push command: "+command.name)
	stack.undo = append(stack.undo, command)
	clear(stack.redo)
	stack.redo = stack.redo[:0]
	stack.balance++
}

// An accepted edit may arrive while a history operation is awaiting its
// acknowledgement. Finish that transition before inserting the new branch.
func (stack *commandStack) flushQueued() {
	for _, command := range stack.queued {
		stack.push(command)
	}
	clear(stack.queued)
	stack.queued = stack.queued[:0]
}

func (stack *commandStack) clear() {
	clear(stack.undo[:cap(stack.undo)])
	clear(stack.redo[:cap(stack.redo)])
	clear(stack.queued[:cap(stack.queued)])
	stack.undo, stack.redo = nil, nil
	stack.queued = nil
}

// APHELION EDIT ADDITION END

/* APHELION EDIT REMOVAL START - COLLABORATION
func (s *Storage) Undo() {
	s.UndoV(s.currentStackId)
}

func (s *Storage) UndoV(id string) {
	if stack, ok := s.commandStacks[id]; ok {
		logStackAction(stack, "undo")

		if len(stack.undo) == 0 {
			log.Print("unable to undo empty stack")
			return
		}

		s.undo(stack)
	} else {
		logNoStackAvailable("undo")
	}
}
APHELION EDIT REMOVAL END */
// APHELION EDIT ADDITION START - COLLABORATION
func (s *Storage) Undo() {
	s.UndoAsyncV(s.currentStackId, nil)
}

func (s *Storage) UndoV(id string) {
	s.UndoAsyncV(id, nil)
}

func (s *Storage) UndoAsync(complete func(error)) bool {
	return s.UndoAsyncV(s.currentStackId, complete)
}

func (s *Storage) UndoAsyncV(id string, complete func(error)) bool {
	stack, ok := s.commandStacks[id]
	if !ok || len(stack.undo) == 0 || stack.busy {
		return false
	}
	command := stack.undo[len(stack.undo)-1]
	stack.busy = true
	finished := false
	command.RunAsync(func(reversed Command, err error) {
		if finished {
			return
		}
		finished = true
		current, exists := s.commandStacks[id]
		if err == nil && exists && current == stack && len(stack.undo) > 0 && stack.undo[len(stack.undo)-1].id == command.id {
			stack.undo[len(stack.undo)-1] = Command{}
			stack.undo = stack.undo[:len(stack.undo)-1]
			stack.redo = append(stack.redo, reversed)
			stack.balance--
		}
		stack.busy = false
		if exists && current == stack {
			stack.flushQueued()
		}
		if complete != nil {
			complete(err)
		}
	})
	return true
}

// APHELION EDIT ADDITION END

func (s *Storage) undo(stack *commandStack) {
	/* APHELION EDIT REMOVAL START - HISTORY LIFETIME
	var command Command
	command, stack.undo = stack.undo[len(stack.undo)-1], stack.undo[:len(stack.undo)-1]
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - HISTORY LIFETIME
	command := stack.undo[len(stack.undo)-1]
	stack.undo[len(stack.undo)-1] = Command{}
	stack.undo = stack.undo[:len(stack.undo)-1]
	// APHELION EDIT ADDITION END
	stack.redo = append(stack.redo, command.Run())
	stack.balance--
}

/* APHELION EDIT REMOVAL START - COLLABORATION
func (s *Storage) Redo() {
	s.RedoV(s.currentStackId)
}

func (s *Storage) RedoV(id string) {
	if stack, ok := s.commandStacks[id]; ok {
		logStackAction(stack, "redo")

		if len(stack.redo) == 0 {
			log.Print("unable to read empty stack")
			return
		}

		s.redo(stack)
	} else {
		logNoStackAvailable("redo")
	}
}
APHELION EDIT REMOVAL END */
// APHELION EDIT ADDITION START - COLLABORATION
func (s *Storage) Redo() {
	s.RedoAsyncV(s.currentStackId, nil)
}

func (s *Storage) RedoV(id string) {
	s.RedoAsyncV(id, nil)
}

func (s *Storage) RedoAsync(complete func(error)) bool {
	return s.RedoAsyncV(s.currentStackId, complete)
}

func (s *Storage) RedoAsyncV(id string, complete func(error)) bool {
	stack, ok := s.commandStacks[id]
	if !ok || len(stack.redo) == 0 || stack.busy {
		return false
	}
	command := stack.redo[len(stack.redo)-1]
	stack.busy = true
	finished := false
	command.RunAsync(func(reversed Command, err error) {
		if finished {
			return
		}
		finished = true
		current, exists := s.commandStacks[id]
		if err == nil && exists && current == stack && len(stack.redo) > 0 && stack.redo[len(stack.redo)-1].id == command.id {
			stack.redo[len(stack.redo)-1] = Command{}
			stack.redo = stack.redo[:len(stack.redo)-1]
			stack.undo = append(stack.undo, reversed)
			stack.balance++
		}
		stack.busy = false
		if exists && current == stack {
			stack.flushQueued()
		}
		if complete != nil {
			complete(err)
		}
	})
	return true
}

// APHELION EDIT ADDITION END

func (s *Storage) redo(stack *commandStack) {
	/* APHELION EDIT REMOVAL START - HISTORY LIFETIME
	var command Command
	command, stack.redo = stack.redo[len(stack.redo)-1], stack.redo[:len(stack.redo)-1]
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - HISTORY LIFETIME
	command := stack.redo[len(stack.redo)-1]
	stack.redo[len(stack.redo)-1] = Command{}
	stack.redo = stack.redo[:len(stack.redo)-1]
	// APHELION EDIT ADDITION END
	stack.undo = append(stack.undo, command.Run())
	stack.balance++
}

func (s *Storage) HasUndo() bool {
	return s.HasUndoV(s.currentStackId)
}

func (s *Storage) HasUndoV(id string) bool {
	if stack, ok := s.commandStacks[id]; ok {
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: return len(stack.undo) > 0
		return !stack.busy && len(stack.undo) > 0
	}
	return false
}

func (s *Storage) HasRedo() bool {
	return s.HasRedoV(s.currentStackId)
}

func (s *Storage) HasRedoV(id string) bool {
	if stack, ok := s.commandStacks[id]; ok {
		// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: return len(stack.redo) > 0
		return !stack.busy && len(stack.redo) > 0
	}
	return false
}

func (s *Storage) IsModified(id string) bool {
	if stack, ok := s.commandStacks[id]; ok {
		// APHELION EDIT CHANGE - HISTORY ORDERING - ORIGINAL: return stack.balance != 0 || stack.appliedCommandId() != stack.balanceCommandId
		return stack.busy || stack.balance != 0 || stack.appliedCommandId() != stack.balanceCommandId
	}
	return false
}

func (s *Storage) ForceBalance(id string) {
	if id == NullSpaceStackId {
		log.Print("skipping force balance for:", id)
		return
	}

	if stack, ok := s.commandStacks[id]; ok {
		logStackAction(stack, "force balance")
		stack.balance = 0
		stack.balanceCommandId = stack.appliedCommandId()
	}
}

func (s *Storage) Balance(id string) {
	if id == NullSpaceStackId {
		log.Print("skipping balance for:", id)
		return
	}

	if stack, ok := s.commandStacks[id]; ok {
		logStackAction(stack, "balance")
		// APHELION EDIT ADDITION START - COLLABORATION
		for _, commands := range [][]Command{stack.undo, stack.redo} {
			for _, command := range commands {
				if command.undoAsync != nil || command.redoAsync != nil {
					log.Print("skip balancing asynchronous command stack")
					return
				}
			}
		}
		// APHELION EDIT ADDITION END

		for {
			if stack.balance == 0 {
				break
			} else if stack.balance > 0 {
				s.undo(stack)
			} else if stack.balance < 0 {
				s.redo(stack)
			}
		}
	}
}

func logNoStackAvailable(action string) {
	log.Print("invalid action, no stack available at the moment:", action)
}

func logStackAction(stack *commandStack, action string) {
	log.Printf("stack action [%s] on [%s]", action, stack.id)
}

type commandStack struct {
	id      string
	balance int
	undo    []Command
	redo    []Command
	// APHELION EDIT ADDITION START - COLLABORATION
	busy   bool
	queued []Command
	// APHELION EDIT ADDITION END

	// Field stores a command id at the moment when the stack was forcefully balanced.
	balanceCommandId uint64
}

func (c commandStack) appliedCommandId() uint64 {
	if len(c.undo) > 0 {
		// APHELION EDIT CHANGE - HISTORY ORDERING - ORIGINAL: return c.undo[0].id
		return c.undo[len(c.undo)-1].id
	}
	return 0
}
