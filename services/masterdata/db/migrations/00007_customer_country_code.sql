-- +goose Up

-- The country as a code, because it is about to become something the system
-- groups by rather than something a person reads.
--
-- customers.country is free text and has been since the first migration. That
-- was fine while it was a label on a form. It stops being fine the moment
-- "send this to every customer in Brazil" exists, because Brazil, 巴西, BR and
-- brasil are then four different countries — and the failure is silent: the
-- screen shows four groups, each looks plausible, and somebody sends a price
-- list to a quarter of the people they meant to.
--
-- ISO 3166-1 alpha-2, and only the code. The *name* is presentation and it is
-- presentation in three languages here; storing "Brazil" would make the list
-- English for a Chinese user forever. Every browser ships the translations
-- already — Intl.DisplayNames turns 'BR' into 巴西 or Brasil or Brazil
-- depending on who is looking — so the code is the whole of what a database
-- needs to hold.
ALTER TABLE customers ADD COLUMN country_code CHAR(2) NOT NULL DEFAULT '';

-- Two uppercase letters or nothing. '' is a real state and a common one: a
-- customer whose country nobody filled in, or one whose free text did not map
-- to anything below. Rejecting it would make this column impossible to add.
ALTER TABLE customers ADD CONSTRAINT customers_country_code_check
    CHECK (country_code = '' OR country_code ~ '^[A-Z]{2}$');

-- Backfill from whatever the free text happens to say.
--
-- Deliberately a short list rather than all 249 countries. What is in the
-- column today is two rows, and the list below covers the spellings an export
-- business actually types plus both languages it types them in. Anything it
-- misses stays empty, shows as 未归类 on screen, and is fixed with a dropdown
-- in a couple of seconds — which is a better outcome than a migration nobody
-- can read carrying two hundred rows that will never match anything.
UPDATE customers SET country_code = m.code
FROM (VALUES
    ('china','CN'), ('中国','CN'), ('prc','CN'), ('p.r. china','CN'), ('cn','CN'),
    ('hong kong','HK'), ('香港','HK'), ('hk','HK'),
    ('taiwan','TW'), ('台湾','TW'), ('tw','TW'),
    ('united states','US'), ('usa','US'), ('u.s.a.','US'), ('america','US'),
    ('united states of america','US'), ('美国','US'), ('us','US'),
    ('canada','CA'), ('加拿大','CA'), ('ca','CA'),
    ('mexico','MX'), ('墨西哥','MX'), ('mx','MX'),
    ('brazil','BR'), ('brasil','BR'), ('巴西','BR'), ('br','BR'),
    ('argentina','AR'), ('阿根廷','AR'), ('ar','AR'),
    ('chile','CL'), ('智利','CL'), ('cl','CL'),
    ('colombia','CO'), ('哥伦比亚','CO'), ('co','CO'),
    ('peru','PE'), ('秘鲁','PE'), ('pe','PE'),
    ('united kingdom','GB'), ('uk','GB'), ('england','GB'), ('britain','GB'),
    ('great britain','GB'), ('英国','GB'), ('gb','GB'),
    ('germany','DE'), ('deutschland','DE'), ('德国','DE'), ('de','DE'),
    ('france','FR'), ('法国','FR'), ('fr','FR'),
    ('italy','IT'), ('italia','IT'), ('意大利','IT'), ('it','IT'),
    ('spain','ES'), ('españa','ES'), ('espana','ES'), ('西班牙','ES'), ('es','ES'),
    ('netherlands','NL'), ('holland','NL'), ('荷兰','NL'), ('nl','NL'),
    ('belgium','BE'), ('比利时','BE'), ('be','BE'),
    ('poland','PL'), ('波兰','PL'), ('pl','PL'),
    ('russia','RU'), ('俄罗斯','RU'), ('ru','RU'),
    ('turkey','TR'), ('türkiye','TR'), ('turkiye','TR'), ('土耳其','TR'), ('tr','TR'),
    ('india','IN'), ('印度','IN'), ('in','IN'),
    ('pakistan','PK'), ('巴基斯坦','PK'), ('pk','PK'),
    ('bangladesh','BD'), ('孟加拉国','BD'), ('孟加拉','BD'), ('bd','BD'),
    ('vietnam','VN'), ('viet nam','VN'), ('越南','VN'), ('vn','VN'),
    ('thailand','TH'), ('泰国','TH'), ('th','TH'),
    ('indonesia','ID'), ('印度尼西亚','ID'), ('印尼','ID'), ('id','ID'),
    ('malaysia','MY'), ('马来西亚','MY'), ('my','MY'),
    ('singapore','SG'), ('新加坡','SG'), ('sg','SG'),
    ('philippines','PH'), ('菲律宾','PH'), ('ph','PH'),
    ('japan','JP'), ('日本','JP'), ('jp','JP'),
    ('south korea','KR'), ('korea','KR'), ('韩国','KR'), ('kr','KR'),
    ('australia','AU'), ('澳大利亚','AU'), ('澳洲','AU'), ('au','AU'),
    ('new zealand','NZ'), ('新西兰','NZ'), ('nz','NZ'),
    ('united arab emirates','AE'), ('uae','AE'), ('dubai','AE'),
    ('阿联酋','AE'), ('ae','AE'),
    ('saudi arabia','SA'), ('沙特阿拉伯','SA'), ('沙特','SA'), ('sa','SA'),
    ('egypt','EG'), ('埃及','EG'), ('eg','EG'),
    ('south africa','ZA'), ('南非','ZA'), ('za','ZA'),
    ('nigeria','NG'), ('尼日利亚','NG'), ('ng','NG'),
    ('kenya','KE'), ('肯尼亚','KE'), ('ke','KE')
) AS m(label, code)
WHERE lower(btrim(customers.country)) = m.label
  AND customers.country_code = '';

-- Grouping runs on every open of the recipient picker, and filters to active
-- customers the way every other list here does.
CREATE INDEX customers_country_idx
    ON customers (tenant_id, country_code)
    WHERE status = 'ACTIVE';

-- +goose Down
DROP INDEX IF EXISTS customers_country_idx;
ALTER TABLE customers DROP CONSTRAINT IF EXISTS customers_country_code_check;
ALTER TABLE customers DROP COLUMN IF EXISTS country_code;
