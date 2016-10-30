[![Build Status](https://drone.io/bitbucket.org/shu/pomi/status.png)](https://drone.io/bitbucket.org/shu/pomi/latest)

# ダウンロード

pomi は、以下の場所からダウンロードできます。

[https://drone.io/bitbucket.org/shu/pomi/files](https://drone.io/bitbucket.org/shu/pomi/files)

# 使い方

0. まず最初に、ポメラSyncのファイルを管理するディレクトリを決めます。
   いくつかやり方はありますが、一番簡単なのは、このディレクトリに pomi の実行ファイルと pomi.toml を作る方法です。
   以下の説明では、ポメラSyncのファイルを管理するディレクトリ＝pomiを配置したディレクトリとしています。
   ※設定ファイルの位置を変更したい場合は、次のセクション「フォルダ構成の例」を参照してください。
1. pomi_sample.toml を pomi.toml にリネームします。
2. pomi.toml をテキストエディターで開き、USER, PASS の項目をご自身の Gmail 設定に合わせて変更します。
3. 動作確認として、コマンドプロンプトやターミナルソフトを立ち上げ、「pomi list」を入力してください。
   ポメラSyncの内容が一覧表示されればOKです。
4. 詳細な使い方については、「pomi help」もしくは「pomi help コマンド」（コマンド＝list や get や put）を入力して説明文を参照してください。

# フォルダ構成の例

## 最初に説明

pomi では、デフォルトではカレントディレクトリの pomi.toml を参照するようにしています。
ですが、グローバルオプション --config で設定ファイルの場所を指定することができます。

    pomi --config 代替となる設定ファイルへのパス  コマンド

デフォルトの設定ファイルは、pomi の実行ファイルがある場所ではなく、あくまでもカレントディレクトリです。
そのため、pomi の実行ファイルそのものはパスの通ったどこかにおいておき、

## 例1 「使い方」で説明したパスの構成

    pomi.zip                ←DL してきた ZIP ファイル
    pomi/                   ←ZIP ファイルを展開してできたフォルダ
        pomi                ←実行ファイル
        pomi_sample.toml
        pomi.toml           ←pomi_sample.toml からリネーム
        メモ１.txt          ←pomi を使って、ポメラSyncから取得したメモ
        メモ２.txt

* pomi/ 内で操作を行います。
* 実行ファイルにパスを通す必要がありません。
* pomi put を実行する際に、間違えて pomi 実行ファイルや pomi.toml を指定しないようにしてください。

## 例2 ポメラSync管理フォルダを設ける

    pomi.zip
    pomi/                   ←予め、ここにパスを通して置く
        pomi
        pomi_sample.toml
    a/b/c/
        pomi.toml
        sync/               ←ここがカレントディレクトリ
          メモ１.txt
          メモ２.txt

* ポメラSyncをする際に、pomi 関係のファイルが入り込む心配がなくなります。
* pomi 実行ファイルを置いたフォルダには、パスを通しておきます。
* pomi を実行するためには 「pomi --config a/b/c/pomi.toml」のようにしないといけません。
  * 利便性のために、上記の内容を記述したシェルスクリプトを作っておくとよいでしょう。

---
>  vim: set et ft=markdown sts=4 sw=4 ts=4 tw=0 : 
