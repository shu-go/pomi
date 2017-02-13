# pomi - Pomera Sync IMAP tool

ポメラSyncされたメモを操作するツールです。

# ダウンロード

以下の場所からダウンロードできます。

[http://goo.gl/T8BFzD](http://goo.gl/T8BFzD)

# 使い始めの設定

pomi はコマンドラインアプリケーションです。
Windows ではコマンドプロンプトを使って操作をします。

## 1. Gmail への接続設定

コマンドプロンプトから pomi auth コマンドを実行して、Gmail に接続します。

    > pomi auth

初回実行時は「Windows セキュリティの重要な警告」が表示されますので、「アクセスを許可する」を押してください。

また、コマンドを実行すると同時にブラウザーが起動し、「pomi が次の許可をリクエストしています:」と表示されますので、内容を確認の上、同意する場合に「許可」をおしてください。

許可後のページは閉じていただいて結構です。（本当は自動的に閉じるようにしたいのですが、Chrome ではできません…）

(pomi auth は、デフォルトでは60秒でタイムアウトします。60秒以上経った場合は、再度 pomi auth を実行してください)

## 2. 接続確認

コマンド pomi list を実行して、ポメラSyncの内容が見えることを確認してください。

    > pomi list
    (以下は表示例)
    1 テスト (Fri, 18 Nov 2016 11:23:22 +0900)
    2 ★メモ★ (Wed, 09 Nov 2016 18:16:46 +0900)

表示例にある左端の番号（1, 2, …）は、ほかのコマンドでも使います。

## 3. 動作確認2 メモの取得

コマンド pomi get を使うと、条件を指定してメモをファイルとして取得できます。

    > pomi get --all

デフォルトの保存先は、「(カレントディレクトリ)/pomera_sync」 となっています。
これは、--dir オプションで変更可能です。

    > pomi --dir a/b/c get --all

## 4. 動作確認3 メモの格納

ローカルにあるファイルをポメラSyncに格納するためには、pomi put コマンドを使います。

(ここでは、ローカルのファイル ★メモ★.txt を編集したものとします)

    > pomi put ★メモ★.txt
    searching files in ./pomera_sync
    putting pomera_sync\★メモ★.txt

ファイルの指定には、ワイルドカードとして * が使えます。

    > pomi put ★*
    searching files in ./pomera_sync
    putting pomera_sync\★メモ★.txt

# 使い方

コマンドラインから、pomi help コマンドを実行して、何ができるか確認してみてください。

※コマンドは今後増える可能性があります。

    > pomi help
    NAME:
      pomi - Pomera Sync IMAP tool

    USAGE:
      pomi [global options] command [command options] [arguments...]

    VERSION:
      0.1.0

    COMMANDS:
      auth            authenticate with gmail
      list, l, ls     list messages
      show, g         show messages
      get, g          get messages
      put, p          put messages
      delete, del, d  delete messages
      help, h         Shows a list of commands or help for one command
    
    GLOBAL OPTIONS:
      --config CONFIG, --conf CONFIG  load the configuration from CONFIG (default: "./pomi.toml")
      --dir DIR, -d DIR               set local directory to DIR (default: "./pomera_sync")
      --help, -h                      show help
      --version, -v                   print the version

また、各コマンドの詳細な使い方は、「pomi help コマンド名」を実行することで確認できます。

    > pomi help list
    NAME:
      pomi list - list messages in the box

    USAGE:
      pomi list [command options] [arguments...]

    OPTIONS:
      --criteria value, -c value  criteria (default: "SUBJECT")

# 更新履歴

* 0.1.2 (2017-02-13)
    - pomi put / get で、ファイルの拡張子を保持*しようとする*仕組みを追加

        たとえば「ほげほげ.md」(中身がマークダウンのテキストファイル) を put して get した場合に、従来の挙動では拡張子が txt になっていましたが、今回の対応で md を保持するようになります。

        X-Pomi-Ext というヘッダーを付与して対応しています。
        他のアプリを使って編集をした場合、このヘッダーが消えてしまうかもしれません。

* 0.1.1 (2017-02-11)
    - pomi put の際に、上書きか新規メッセージかを判断する基準をより細かくしました。

        Subject が対象のファイル名(拡張子を除く)に厳密一致する場合は上書きになります。

* 0.1.0

---
>  vim: set et ft=markdown sts=4 sw=4 ts=4 tw=0 :
