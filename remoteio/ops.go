package remoteio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
)

// Copy は src から読み取った内容を dst へ書き込みます。スキームは跨げます
// （gs:// から s3://、リモートからローカル、いずれも同じ呼び出しです）。
//
// 中身をメモリに読み切らず、Reader から Writer へストリームで渡します。
//
// 書き込み時のメタデータはコピー元を見ずに WriteOption だけで決まります。
// 暗黙に HEAD 相当の問い合わせを増やさないためです。Content-Type を引き継ぎたい場合は
// Stat の結果を WithContentType へ渡してください。
func Copy(ctx context.Context, reader Reader, writer Writer, src, dst string, opts ...WriteOption) error {
	rc, err := reader.Open(ctx, src)
	if err != nil {
		return fmt.Errorf("コピー元のオープンに失敗しました (%s): %w", src, err)
	}
	defer func() { _ = rc.Close() }()

	if err := writer.Write(ctx, dst, rc, opts...); err != nil {
		return fmt.Errorf("コピー先への書き込みに失敗しました (%s -> %s): %w", src, dst, err)
	}
	return nil
}

// Move は Copy のあとにコピー元を削除します。
// コピーが成功した場合にのみ削除するため、途中で失敗してもコピー元は残ります。
func Move(ctx context.Context, reader Reader, writer interface {
	Writer
	Remover
}, src, dst string, opts ...WriteOption) error {
	if err := Copy(ctx, reader, writer, src, dst, opts...); err != nil {
		return err
	}
	if err := writer.Delete(ctx, src); err != nil {
		return fmt.Errorf("コピー元の削除に失敗しました (%s): %w", src, err)
	}
	return nil
}

// Stat は、reader がメタデータ取得に対応していればそれを返します。
//
// Stater を InputReader へ含めていないのは、複合インターフェースを実装している
// 既存のフェイクや代替実装を壊さないためです。この関数はその型アサーションを
// 呼び出し側ごとに書かなくて済むようにするものです。*Router は対応しています。
func Stat(ctx context.Context, reader Reader, path string) (ObjectInfo, error) {
	stater, ok := reader.(Stater)
	if !ok {
		return ObjectInfo{}, fmt.Errorf("この Reader はメタデータ取得に対応していません: %T", reader)
	}
	return stater.Stat(ctx, path)
}

// errStopIteration は Files がイテレーションの打ち切りを List へ伝えるための番兵です。
var errStopIteration = errors.New("remoteio: イテレーションを打ち切りました")

// Files は List をイテレータとして返します。
//
//	for path, err := range remoteio.Files(ctx, reader, "gs://bucket/data") {
//		if err != nil {
//			return err
//		}
//		...
//	}
//
// callback 版と両立させているのは、range over func が使えない書き方
// （callback をそのまま他所へ渡す等）が既にあるためです。実装は List 側に 1 つだけで、
// これはその薄い包みです。break や return で抜けると List も打ち切られます。
// エラーは最後の 1 回だけ、空パスと共に渡されます。
func Files(ctx context.Context, lister Lister, path string, opts ...ListOption) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		err := lister.List(ctx, path, func(found string) error {
			if !yield(found, nil) {
				return errStopIteration
			}
			return nil
		}, opts...)

		if err != nil && !errors.Is(err, errStopIteration) {
			yield("", err)
		}
	}
}

// ReadAll は path の内容をすべて読み取って返します。
// Open と Close の組を毎回書かずに済ませるための補助です。
func ReadAll(ctx context.Context, reader Reader, path string) ([]byte, error) {
	rc, err := reader.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("読み込みに失敗しました (%s): %w", path, err)
	}
	return data, nil
}
