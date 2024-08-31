package ast

import "github.com/delavalom/arvlang/lang/tokens"

type Block struct {
	Unscoped   bool
	Statements []Node
}

func NewBlock() *Block {
	return &Block{
		Unscoped:   false,
		Statements: []Node{},
	}
}

func (p *Block) GetToken() *tokens.Token {
	return nil
}

func (p *Block) String() string {
	return "<block>"
}

func (p *Block) Children() []Node {
	return p.Statements
}

func (p *Block) Traverse(level int, fn func(int, Node)) {
	fn(level, p)

	for _, s := range p.Children() {
		s.Traverse(level+1, fn)
	}
}
