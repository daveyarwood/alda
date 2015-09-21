(ns alda.parser
  (:require [instaparse.core :as insta]
            [clojure.string  :as str]
            [clojure.java.io :as io]
            [alda.util       :as util]
            [backtick        :refer (defquote)]))

; sets log level to TIMBRE_LEVEL (if set) or :warn
(util/set-timbre-level!)

(declare alda-parser parse-input)
(require '[alda.lisp :as lisp])

(def ^:private alda-parser
  (insta/parser (io/resource "alda.bnf")))

(defn parse-tree
  "Returns the intermediate parse tree resulting from parsing a string of Alda
   code."
  [alda-code]
  (alda-parser alda-code))

(defquote alda-lisp-quote
  #(if-let [{:keys [ns name]} (meta (ns-resolve 'alda.lisp %))]
     (symbol (str ns "/" name))
     %))

(defn read-to-alda-lisp
  [code]
  (load-string (format "(alda.parser/alda-lisp-quote %s)" code)))

(defn- read-clj-expr
  "Reads an inline Clojure expression within Alda code.
   
   Special rules:
     - unless it's a character literal or inside a string, each comma or
       semicolon will split an S-expression, e.g.:
         (volume 50, quant 50) => (do (volume 50) (quant 50))
     - symbols will first try to be resolved within the context of alda.lisp,
       then if that fails, the current run-time namespace

   (To do this, we use some regex voodoo adapted from:
   http://stackoverflow.com/a/6464500/2338327)

   Returns ready-to-evaluate Clojure code."
  [expr]
  (let [expr   (apply str expr)
        voodoo #"(?<!\\)[,;](?=([^\"\\]*(\\.|\"([^\"\\]*\\.)*[^\"\\]*\"))*[^\"]*\z)"
        exprs (->> (str/split expr voodoo)
                   (map #(str \( % \))))]
    (if (> (count exprs) 1)
      (cons 'do (map read-to-alda-lisp exprs))
      (read-to-alda-lisp (first exprs)))))

(defn parse-input
  "Parses a string of Alda code and turns it into Clojure code."
  [alda-code]
  (->> alda-code
       alda-parser
       (insta/transform
         {:clj-expr          #(read-clj-expr %&)
          :name              #(hash-map :name %)
          :nickname          #(hash-map :nickname %)
          :number            #(Integer/parseInt %)
          :voice-number      #(Integer/parseInt %)
          :tie               (constantly :tie)
          :slur              (constantly :slur)
          :flat              (constantly :flat)
          :sharp             (constantly :sharp)
          :dots              #(hash-map :dots (count %))
          :note-length       #(list* 'alda.lisp/note-length %&)
          :duration          #(list* 'alda.lisp/duration %&)
          :pitch             (fn [s]
                               (list* 'alda.lisp/pitch
                                      (keyword (str (first s)))
                                      (map #(case %
                                              \- :flat
                                              \+ :sharp)
                                           (rest s))))
          :note              #(list* 'alda.lisp/note %&)
          :rest              #(list* 'alda.lisp/pause %&)
          :chord             #(list* 'alda.lisp/chord %&)
          :octave-set        #(list 'alda.lisp/octave %)
          :octave-up         #(list 'alda.lisp/octave :up)
          :octave-down       #(list 'alda.lisp/octave :down)
          :voice             #(list* 'alda.lisp/voice %&)
          :voices            #(list* 'alda.lisp/voices %&)
          :marker            #(list 'alda.lisp/marker (:name %))
          :at-marker         #(list 'alda.lisp/at-marker (:name %))
          :barline           #(list 'alda.lisp/barline)
          :calls             (fn [& calls]
                               (let [names    (vec (keep :name calls))
                                     nickname (some :nickname calls)]
                                 (if nickname
                                   {:names names, :nickname nickname}
                                   {:names names})))
          :part              #(list* 'alda.lisp/part %&)
          :score             #(list* 'alda.lisp/score %&)})))
